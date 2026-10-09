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
)

// Config contains the domain configuration of the snowflake generator.
type Config struct {
	LeafName string
}

// Manager generates snowflake IDs. Each biz_tag owns an independent worker-ID
// space under leaf_name/biz_tag.
type Manager struct {
	mu         sync.Mutex
	cfg        Config
	newInfra   TagInfraFactory
	generators map[string]*tagGenerator
	closed     bool
	closeOnce  sync.Once
}

// NewManager creates a manager. Worker slots are acquired lazily for each
// biz_tag. A nil factory means no infrastructure at all: generators fall back
// to the local worker-slot cache semantics without any etcd interaction.
func NewManager(cfg Config, factory TagInfraFactory) (*Manager, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	if factory == nil {
		factory = func(string) TagInfra { return TagInfra{Cache: noopCache{}} }
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

	infra := m.newInfra(bizTag)
	if infra.Cache == nil {
		infra.Cache = noopCache{}
	}
	generator, err := newTagGenerator(ctx, infra, m.cfg, bizTag)
	if err != nil {
		return nil, err
	}
	m.generators[bizTag] = generator
	return generator, nil
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
