// Package idgen implements the ID-generation domain: the local part of
// Leaf-snowflake (41-bit timestamp, 10-bit worker ID, 12-bit sequence).
// etcd coordination and the worker-slot disk cache are injected through
// the ports in ports.go and provided by the infrastructure layer
// (etcdcoord / workerfile).
package idgen

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DefaultMaxBatchCount is the default upper bound for a single BatchNext call.
const DefaultMaxBatchCount = 100000

// Config contains the domain configuration of the snowflake generator.
type Config struct {
	LeafName string

	// AllowLocalFallback enables the degraded local-worker path when etcd is
	// unreachable. Default false (fail closed): without this flag an etcd
	// outage rejects ID generation instead of risking duplicates.
	AllowLocalFallback bool

	// MaxBizTagLen is the maximum byte length of a biz_tag (default 64).
	MaxBizTagLen int
	// MaxGenerators is the maximum number of concurrent per-tag generators
	// (default 1024).
	MaxGenerators int
	// MaxBatchCount is the maximum count accepted by BatchNext (default 100000).
	MaxBatchCount int

	// WatermarkSafetyMargin is how far ahead of the current timestamp the
	// persisted watermark is written (default 5s). Must exceed
	// WatermarkPersistInterval to cover a crash between persists.
	WatermarkSafetyMargin time.Duration
	// WatermarkPersistInterval is how often the watermark is persisted
	// (default 3s).
	WatermarkPersistInterval time.Duration
	// WatermarkMaxWait is the maximum time a restarting process will wait
	// for the persisted watermark window to pass (default 30s). A longer
	// wait means the clock is far behind and startup is rejected.
	WatermarkMaxWait time.Duration
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.LeafName == "" {
		cfg.LeafName = "devops-id"
	}
	if err := validateName(cfg.LeafName, cfg.MaxBizTagLen); err != nil {
		return Config{}, fmt.Errorf("invalid leaf_name: %w", err)
	}
	if cfg.MaxBizTagLen <= 0 {
		cfg.MaxBizTagLen = 64
	}
	if cfg.MaxGenerators <= 0 {
		cfg.MaxGenerators = 1024
	}
	if cfg.MaxBatchCount <= 0 {
		cfg.MaxBatchCount = DefaultMaxBatchCount
	}
	if cfg.WatermarkSafetyMargin <= 0 {
		cfg.WatermarkSafetyMargin = 5 * time.Second
	}
	if cfg.WatermarkPersistInterval <= 0 {
		cfg.WatermarkPersistInterval = 3 * time.Second
	}
	if cfg.WatermarkMaxWait <= 0 {
		cfg.WatermarkMaxWait = 30 * time.Second
	}
	if cfg.WatermarkSafetyMargin <= cfg.WatermarkPersistInterval {
		return Config{}, fmt.Errorf("watermark_safety_margin (%v) must exceed watermark_persist_interval (%v)",
			cfg.WatermarkSafetyMargin, cfg.WatermarkPersistInterval)
	}
	return cfg, nil
}

func validateName(name string, maxLen int) error {
	if maxLen <= 0 {
		maxLen = 64
	}
	if name == "" || strings.TrimSpace(name) != name || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: %q must be a safe path component", ErrInvalidName, name)
	}
	if len(name) > maxLen {
		return fmt.Errorf("%w: %q length %d exceeds maximum %d", ErrInvalidName, name, len(name), maxLen)
	}
	return nil
}

// initCall coordinates per-tag single-flight generator initialization.
type initCall struct {
	done chan struct{}
}

// Manager generates snowflake IDs. Each biz_tag owns an independent worker-ID
// space under leaf_name/biz_tag.
type Manager struct {
	mu         sync.Mutex
	cfg        Config
	newInfra   TagInfraFactory
	generators map[string]*tagGenerator
	inflight   map[string]*initCall
	states     map[string]HealthState
	obs        Observer
	closed     bool
	closeOnce  sync.Once
}

// NewManager creates a manager. Worker slots are acquired lazily for each
// biz_tag. A nil factory means no infrastructure at all: generators run in
// local mode (cached slot or worker 0). A nil observer discards all events.
func NewManager(cfg Config, factory TagInfraFactory, obs Observer) (*Manager, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if factory == nil {
		factory = func(string) TagInfra { return TagInfra{Cache: noopCache{}} }
	}
	if obs == nil {
		obs = noopObserver{}
	}
	return &Manager{
		cfg:        cfg,
		newInfra:   factory,
		generators: make(map[string]*tagGenerator),
		inflight:   make(map[string]*initCall),
		states:     make(map[string]HealthState),
		obs:        obs,
	}, nil
}

// Config returns the normalized domain configuration.
func (m *Manager) Config() Config {
	return m.cfg
}

// Next generates one ID in the independent worker space of biz_tag.
func (m *Manager) Next(ctx context.Context, bizTag string) (int64, error) {
	generator, err := m.generator(ctx, bizTag)
	if err != nil {
		return 0, err
	}
	return generator.next(ctx)
}

// BatchNext generates a monotonic exclusive batch of IDs for one biz_tag:
// strictly increasing, and no concurrent caller's ID falls inside [first, last].
// IDs are not contiguous across millisecond boundaries.
func (m *Manager) BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error) {
	if count < 1 {
		return nil, fmt.Errorf("count must be >= 1: %d", count)
	}
	if count > m.cfg.MaxBatchCount {
		return nil, fmt.Errorf("%w: %d > %d", ErrBatchTooLarge, count, m.cfg.MaxBatchCount)
	}
	generator, err := m.generator(ctx, bizTag)
	if err != nil {
		return nil, err
	}
	return generator.batchNext(ctx, count)
}

// generator returns the per-tag generator, creating it on first use.
// Per-tag single-flight: concurrent requests for the same tag share one
// initialization; different tags initialize concurrently. The global lock
// only protects map operations — network and disk I/O happen outside it.
func (m *Manager) generator(ctx context.Context, bizTag string) (*tagGenerator, error) {
	if err := validateName(bizTag, m.cfg.MaxBizTagLen); err != nil {
		return nil, fmt.Errorf("invalid biz_tag: %w", err)
	}

	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrManagerClosed
		}
		if generator, ok := m.generators[bizTag]; ok {
			m.mu.Unlock()
			return generator, nil
		}
		if call, ok := m.inflight[bizTag]; ok {
			m.mu.Unlock()
			select {
			case <-call.done:
				continue // re-check: init may have succeeded or failed
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if len(m.generators)+len(m.inflight) >= m.cfg.MaxGenerators {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %d", ErrTooManyTags, m.cfg.MaxGenerators)
		}
		call := &initCall{done: make(chan struct{})}
		m.inflight[bizTag] = call
		m.mu.Unlock()

		// Slow I/O outside the lock: lease grant, etcd slot acquire, disk writes.
		infra := m.newInfra(bizTag)
		if infra.Cache == nil {
			infra.Cache = noopCache{}
		}
		generator, err := newTagGenerator(ctx, infra, m.cfg, bizTag, m.obs)

		m.mu.Lock()
		delete(m.inflight, bizTag)
		if err == nil {
			if m.closed {
				m.mu.Unlock()
				generator.Close()
				close(call.done)
				return nil, ErrManagerClosed
			}
			m.generators[bizTag] = generator
			st := HealthServing
			if generator.fallback {
				st = HealthDegraded
			}
			m.states[bizTag] = st
			// Emit the initial health state. Without this, idgen_health_state
			// never receives a sample on the happy path (only degradation and
			// close events fire), so the gauge stays empty on a healthy
			// service and monitoring shows "No data" instead of "serving".
			m.obs.OnState(bizTag, st)
		}
		m.mu.Unlock()
		close(call.done)
		return generator, err
	}
}

// OverallHealth returns the worst health state across all generators.
func (m *Manager) OverallHealth() HealthState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return HealthNotServing
	}
	worst := HealthServing
	for _, st := range m.states {
		if st > worst {
			worst = st
		}
	}
	return worst
}

// Close cancels refreshes and revokes every per-tag lease immediately.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		generators := make([]*tagGenerator, 0, len(m.generators))
		for _, generator := range m.generators {
			generators = append(generators, generator)
		}
		m.mu.Unlock()

		for _, generator := range generators {
			generator.Close()
		}
		m.obs.OnState("", HealthNotServing)
	})
}
