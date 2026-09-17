// Package snowflake implements the local part of Leaf-snowflake: 41-bit
// timestamp, 10-bit worker ID, and 12-bit sequence. etcd coordination is
// provided by devops-infra.
package snowflake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	etcdinfra "devops-infra/etcd"
)

const (
	workerBits = 10
	seqBits    = 12
	maxWorker  = -1 ^ (-1 << workerBits) // 1023
	maxSeq     = -1 ^ (-1 << seqBits)    // 4095

	// epoch is 2024-01-01T00:00:00Z in milliseconds.
	epoch = int64(1704067200000)

	slotRoot = "idgen/snowflake"

	// maxBackwardMs is the tolerated clock rollback threshold.
	maxBackwardMs = int64(5)
)

// ErrClockBackward means clock rollback exceeded the safe threshold.
var ErrClockBackward = errors.New("clock rollback exceeded threshold, reject id generation")

// ErrLeaseLost means the lease-backed worker slot may have been taken over.
var ErrLeaseLost = errors.New("lease lost, worker slot may be occupied by another instance")

// ErrInvalidName means leaf_name or biz_tag cannot be used as an etcd/cache path component.
var ErrInvalidName = errors.New("invalid leaf_name or biz_tag")

// ErrManagerClosed means the snowflake manager has already been closed.
var ErrManagerClosed = errors.New("snowflake manager is closed")

// Config contains snowflake-specific configuration.
type Config struct {
	LeafName    string
	LeaseTTL    time.Duration
	WorkerCache string
}

type leaseLifecycle interface {
	Grant(ctx context.Context) error
	Lost() <-chan struct{}
	Release(ctx context.Context) error
}

type slotCoordinator interface {
	Acquire(ctx context.Context, keyPrefix, identity string, count int) (int, error)
	Refresh(ctx context.Context, keyPrefix, identity string, slot, timestamp int64) error
}

type infraFactory func() (leaseLifecycle, slotCoordinator)

// Manager generates snowflake IDs. Each biz_tag owns an independent worker-ID
// space under leaf_name/biz_tag.
type Manager struct {
	mu         sync.Mutex
	cfg        Config
	newInfra   infraFactory
	generators map[string]*tagGenerator
	closed     bool
	closeOnce  sync.Once
}

// tagGenerator holds worker and sequence state for one biz_tag.
type tagGenerator struct {
	mu            sync.Mutex
	lastTimestamp int64
	sequence      int64
	workerID      int64

	lease      leaseLifecycle
	slots      slotCoordinator
	nodeCancel context.CancelFunc
	leaseLost  atomic.Bool
	closed     atomic.Bool

	workerFile string
	slotPrefix string
	instance   string
	closeOnce  sync.Once
}

// New creates a manager. Worker slots are acquired lazily for each biz_tag.
func New(_ context.Context, client *etcdinfra.Client, cfg Config) (*Manager, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	var factory infraFactory
	if client != nil {
		leaseTTL := cfg.LeaseTTL
		factory = func() (leaseLifecycle, slotCoordinator) {
			lease := etcdinfra.NewLeaseManager(client, leaseTTL)
			return lease, etcdinfra.NewSlotCoordinator(client, lease)
		}
	}
	return newManagerWithInfra(cfg, factory)
}

func newManagerWithInfra(cfg Config, factory infraFactory) (*Manager, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if factory == nil {
		factory = func() (leaseLifecycle, slotCoordinator) { return nil, nil }
	}
	return &Manager{
		cfg:        cfg,
		newInfra:   factory,
		generators: make(map[string]*tagGenerator),
	}, nil
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.LeafName == "" {
		cfg.LeafName = "devops-id"
	}
	if err := validateName(cfg.LeafName); err != nil {
		return Config{}, fmt.Errorf("invalid leaf_name: %w", err)
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 10 * time.Second
	}
	if cfg.WorkerCache == "" {
		cfg.WorkerCache = "data"
	}
	return cfg, nil
}

func validateName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: %q must be a safe path component", ErrInvalidName, name)
	}
	return nil
}

// Next generates one ID in the independent worker space of biz_tag.
func (m *Manager) Next(ctx context.Context, bizTag string) (int64, error) {
	generator, err := m.generator(ctx, bizTag)
	if err != nil {
		return 0, err
	}
	return generator.next()
}

// BatchNext generates strictly increasing IDs for one biz_tag.
func (m *Manager) BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error) {
	if count < 1 {
		return nil, fmt.Errorf("count must be >= 1: %d", count)
	}
	generator, err := m.generator(ctx, bizTag)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, count)
	for i := 0; i < count; i++ {
		id, err := generator.next()
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func (m *Manager) generator(ctx context.Context, bizTag string) (*tagGenerator, error) {
	if err := validateName(bizTag); err != nil {
		return nil, fmt.Errorf("invalid biz_tag: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrManagerClosed
	}
	if generator, ok := m.generators[bizTag]; ok {
		return generator, nil
	}

	lease, slots := m.newInfra()
	generator, err := newTagGenerator(ctx, lease, slots, m.cfg, bizTag)
	if err != nil {
		return nil, err
	}
	m.generators[bizTag] = generator
	return generator, nil
}

func newTagGenerator(
	ctx context.Context,
	lease leaseLifecycle,
	slots slotCoordinator,
	cfg Config,
	bizTag string,
) (*tagGenerator, error) {
	if err := validateName(bizTag); err != nil {
		return nil, fmt.Errorf("invalid biz_tag: %w", err)
	}

	generator := &tagGenerator{
		lease:      lease,
		slots:      slots,
		workerFile: filepath.Join(cfg.WorkerCache, cfg.LeafName, bizTag, "worker_id"),
		slotPrefix: strings.Join([]string{slotRoot, cfg.LeafName, bizTag}, "/") + "/",
		instance:   instanceID(),
	}

	var workerID int64
	var err error
	usedLocalFallback := false
	if lease == nil || slots == nil {
		err = etcdinfra.ErrEtcdUnavailable
	} else if grantErr := lease.Grant(ctx); grantErr != nil {
		slog.Warn("grant etcd lease failed, using local worker fallback", "err", grantErr)
		err = etcdinfra.ErrEtcdUnavailable
	} else {
		slot, acquireErr := slots.Acquire(ctx, generator.slotPrefix, generator.instance, maxWorker+1)
		workerID = int64(slot)
		err = acquireErr
	}

	if errors.Is(err, etcdinfra.ErrEtcdUnavailable) {
		usedLocalFallback = true
		if releaseErr := generator.releaseLease(); releaseErr != nil {
			slog.Warn("release etcd lease after fallback failed", "err", releaseErr)
		}
		if initErr := generator.initLocalCache(); initErr != nil {
			return nil, initErr
		}
		workerID, err = generator.acquireLocalSlot()
	}
	if err != nil {
		if releaseErr := generator.releaseLease(); releaseErr != nil {
			slog.Warn("release etcd lease failed", "err", releaseErr)
		}
		return nil, err
	}

	generator.workerID = workerID
	generator.lastTimestamp = time.Now().UnixMilli()
	if err := generator.initLocalCache(); err != nil {
		slog.Warn("initialize worker cache failed", "err", err)
	} else {
		writeIntFile(generator.workerFile, workerID)
	}

	if !usedLocalFallback {
		nodeCtx, cancel := context.WithCancel(context.Background())
		generator.nodeCancel = cancel
		go generator.watchLease(nodeCtx)
		go generator.refreshPermanentNode(nodeCtx)
	}

	slog.Info("snowflake tag generator ready",
		"leaf_name", cfg.LeafName,
		"biz_tag", bizTag,
		"worker_id", workerID,
		"epoch_ms", epoch,
		"local_fallback", usedLocalFallback)
	return generator, nil
}

func (g *tagGenerator) next() (int64, error) {
	if g.closed.Load() {
		return 0, ErrManagerClosed
	}
	if g.leaseLost.Load() {
		return 0, ErrLeaseLost
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().UnixMilli()
	if now < g.lastTimestamp {
		rollback := g.lastTimestamp - now
		if rollback <= maxBackwardMs {
			deadline := time.Now().Add(time.Duration(rollback)*time.Millisecond + time.Second)
			for now < g.lastTimestamp {
				if time.Now().After(deadline) {
					return 0, fmt.Errorf("%w: rollback %dms did not recover", ErrClockBackward, rollback)
				}
				now = time.Now().UnixMilli()
			}
		} else {
			return 0, fmt.Errorf("%w: rollback %dms", ErrClockBackward, rollback)
		}
	}

	if now == g.lastTimestamp {
		g.sequence = (g.sequence + 1) & maxSeq
		if g.sequence == 0 {
			for now <= g.lastTimestamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		g.sequence = 0
	}
	g.lastTimestamp = now

	return ((now - epoch) << (workerBits + seqBits)) | (g.workerID << seqBits) | g.sequence, nil
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
	})
}

func (g *tagGenerator) Close() {
	g.closeOnce.Do(func() {
		g.closed.Store(true)
		if g.nodeCancel != nil {
			g.nodeCancel()
		}
		if err := g.releaseLease(); err != nil {
			slog.Warn("revoke etcd lease failed, slot will expire by TTL",
				"err", err, "worker_id", g.workerID)
		}
		slog.Info("snowflake tag generator closed", "worker_id", g.workerID)
	})
}

func (g *tagGenerator) releaseLease() error {
	if g.lease == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return g.lease.Release(ctx)
}

func (g *tagGenerator) watchLease(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-g.lease.Lost():
		g.leaseLost.Store(true)
		slog.Error("etcd lease lost, id generation is disabled", "worker_id", g.workerID)
	}
}

func (g *tagGenerator) acquireLocalSlot() (int64, error) {
	if cached, ok := readIntFile(g.workerFile); ok && cached >= 0 && cached <= maxWorker {
		slog.Warn("etcd unavailable, reuse cached worker slot", "slot", cached)
		return cached, nil
	}
	writeIntFile(g.workerFile, 0)
	slog.Warn("etcd unavailable and no cached slot, use worker slot 0")
	return 0, nil
}

func (g *tagGenerator) initLocalCache() error {
	dir := filepath.Dir(g.workerFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory %s: %w", dir, err)
	}
	return nil
}

func (g *tagGenerator) refreshPermanentNode(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := g.slots.Refresh(updateCtx, g.slotPrefix, g.instance, g.workerID, time.Now().UnixMilli())
			cancel()
			if err != nil {
				slog.Warn("refresh permanent worker node failed", "err", err)
			}
		}
	}
}

func instanceID() string {
	host, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return host
}

func readIntFile(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func writeIntFile(path string, value int64) {
	if err := os.WriteFile(path, []byte(strconv.FormatInt(value, 10)), 0o644); err != nil {
		slog.Warn("write worker cache failed", "path", path, "err", err)
	}
}
