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

// WorkerCache persists worker-slot and time-watermark state on local disk so
// the generator can recover safely after a crash and, when explicitly allowed,
// fall back to the cached slot when coordination infrastructure is unavailable.
type WorkerCache interface {
	// Ensure creates the cache directory. In the local-fallback path a hard
	// failure is fatal; the etcd path only warns.
	Ensure() error
	// Load returns the cached worker slot; ok is false when missing or corrupt.
	Load() (int64, bool)
	// Save persists the worker slot. In the fallback path a failure is fatal;
	// on the etcd path it is warn-only.
	Save(value int64) error
	// LoadWatermark returns the persisted time watermark in Unix milliseconds;
	// ok is false when missing or corrupt.
	LoadWatermark() (int64, bool)
	// SaveWatermark atomically persists the time watermark in Unix milliseconds.
	SaveWatermark(ts int64) error
}

// HealthState describes the aggregate generation capability of a generator
// or the whole manager.
type HealthState int

const (
	// HealthServing means IDs are being generated normally.
	HealthServing HealthState = iota
	// HealthDegraded means IDs are available but with reduced guarantees
	// (local fallback active, or watermark persistence failing).
	HealthDegraded
	// HealthNotServing means the generator cannot safely produce IDs.
	HealthNotServing
)

// String returns a stable name for logging and metrics.
func (s HealthState) String() string {
	switch s {
	case HealthServing:
		return "serving"
	case HealthDegraded:
		return "degraded"
	case HealthNotServing:
		return "not_serving"
	default:
		return "unknown"
	}
}

// Observer receives domain events for metrics and health reporting.
// All methods must be safe for concurrent use. A nil Observer is valid
// and all events are discarded (see noopObserver).
type Observer interface {
	// OnFallbackActive is called when a generator enters local-fallback mode.
	OnFallbackActive(bizTag string)
	// OnLeaseLost is called when a generator's etcd lease is lost.
	OnLeaseLost(bizTag string)
	// OnClockRollback is called when ID generation is rejected due to clock rollback.
	OnClockRollback(bizTag string, rollbackMs int64)
	// OnState is called when the health state of a generator (or the whole
	// manager when bizTag is "") changes.
	OnState(bizTag string, state HealthState)
}

// TagInfra bundles the per-biz_tag infrastructure handed to each generator.
// A nil Lease/Slots means "no coordination infrastructure configured"
// (local mode); when infrastructure IS configured but unreachable the
// generator follows the fail-closed / opt-in-fallback policy.
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

func (noopCache) Ensure() error                { return nil }
func (noopCache) Load() (int64, bool)          { return 0, false }
func (noopCache) Save(int64) error             { return nil }
func (noopCache) LoadWatermark() (int64, bool) { return 0, false }
func (noopCache) SaveWatermark(int64) error    { return nil }

// noopObserver discards all domain events.
type noopObserver struct{}

func (noopObserver) OnFallbackActive(string)       {}
func (noopObserver) OnLeaseLost(string)            {}
func (noopObserver) OnClockRollback(string, int64) {}
func (noopObserver) OnState(string, HealthState)   {}

// ensure noop types satisfy the ports at compile time.
var (
	_ WorkerCache = noopCache{}
	_ Observer    = noopObserver{}
)
