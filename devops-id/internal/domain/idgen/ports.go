package idgen

import "context"

// LeaseLifecycle is the per-tag etcd lease port: grant with keepalive,
// observe loss, and release.
type LeaseLifecycle interface {
	Grant(ctx context.Context) error
	Lost() <-chan struct{}
	Release(ctx context.Context) error
}

// SlotCoordinator is the worker-slot coordination port backed by etcd.
type SlotCoordinator interface {
	Acquire(ctx context.Context, keyPrefix, identity string, count int) (int, error)
	Refresh(ctx context.Context, keyPrefix, identity string, slot, timestamp int64) error
}

// WorkerCache persists the last worker slot on local disk so the generator
// can reuse it when the coordination infrastructure is unavailable.
type WorkerCache interface {
	// Ensure creates the cache directory. In the local-fallback path a hard
	// failure is fatal; the etcd path only warns.
	Ensure() error
	// Load returns the cached worker slot; ok is false when missing or corrupt.
	Load() (int64, bool)
	// Save persists the worker slot; failures are warn-only.
	Save(value int64)
}

// TagInfra bundles the per-biz_tag infrastructure handed to each generator.
// A nil Lease/Slots means "infrastructure unavailable" and triggers the local
// worker-slot fallback.
type TagInfra struct {
	Lease LeaseLifecycle
	Slots SlotCoordinator
	Cache WorkerCache
}

// TagInfraFactory builds the per-biz_tag infrastructure lazily, on first use
// of each biz_tag.
type TagInfraFactory func(bizTag string) TagInfra

// noopCache is the in-memory no-op WorkerCache used when no cache is
// provided (nil-factory managers, tests): nothing is persisted.
type noopCache struct{}

func (noopCache) Ensure() error       { return nil }
func (noopCache) Load() (int64, bool) { return 0, false }
func (noopCache) Save(int64)          {}
