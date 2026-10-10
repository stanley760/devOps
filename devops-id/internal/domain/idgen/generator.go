package idgen

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

	// msWaitDeadline is the maximum time to wait for the next millisecond
	// (sequence exhaustion or small clock rollback recovery).
	msWaitDeadline = time.Second

	// ctxCheckInterval is how many IDs to generate before checking ctx in a batch.
	ctxCheckInterval = 1024
)

// tagGenerator holds worker and sequence state for one biz_tag.
type tagGenerator struct {
	mu            sync.Mutex
	lastTimestamp int64
	sequence      int64
	workerID      int64

	lease      LeaseLifecycle
	slots      SlotCoordinator
	cache      WorkerCache
	nodeCancel context.CancelFunc
	leaseLost  atomic.Bool
	closed     atomic.Bool

	slotPrefix string
	instance   string
	bizTag     string
	closeOnce  sync.Once

	cfg      Config
	obs      Observer
	fallback bool // true when running in local-fallback (degraded) mode

	persistFailures int // consecutive watermark persist failures (guarded by mu)

	// nowFn returns the current Unix millisecond. Injectable for tests.
	nowFn func() int64
}

func newTagGenerator(
	ctx context.Context,
	infra TagInfra,
	cfg Config,
	bizTag string,
	obs Observer,
) (*tagGenerator, error) {
	if err := validateName(bizTag, cfg.MaxBizTagLen); err != nil {
		return nil, fmt.Errorf("invalid biz_tag: %w", err)
	}
	if obs == nil {
		obs = noopObserver{}
	}

	generator := &tagGenerator{
		lease:      infra.Lease,
		slots:      infra.Slots,
		cache:      infra.Cache,
		slotPrefix: strings.Join([]string{slotRoot, cfg.LeafName, bizTag}, "/") + "/",
		instance:   instanceID(),
		bizTag:     bizTag,
		cfg:        cfg,
		obs:        obs,
		nowFn:      func() int64 { return time.Now().UnixMilli() },
	}

	localMode := generator.lease == nil || generator.slots == nil

	var workerID int64
	var err error

	if localMode {
		// Deliberate local mode (no coordination infra configured): use cached
		// slot or worker 0. This is the dev/test single-instance path.
		workerID, err = generator.acquireLocalSlot()
	} else {
		workerID, err = generator.acquireEtcdSlot(ctx)
		if err != nil && errors.Is(err, ErrInfraUnavailable) && ctx.Err() == nil {
			// Degradation path: infra configured but unreachable.
			if !cfg.AllowLocalFallback {
				slog.Warn("etcd unavailable and local fallback disabled (fail-closed), rejecting id generation",
					"biz_tag", bizTag, "err", err)
				if releaseErr := generator.releaseLease(); releaseErr != nil {
					slog.Warn("release etcd lease after fail-closed rejection failed", "err", releaseErr)
				}
				return nil, err
			}
			// Opt-in fallback: use cached slot only, never slot 0.
			slog.Warn("etcd unavailable, entering local fallback (degraded)", "biz_tag", bizTag, "err", err)
			if releaseErr := generator.releaseLease(); releaseErr != nil {
				slog.Warn("release etcd lease after fallback failed", "err", releaseErr)
			}
			workerID, err = generator.acquireLocalSlotStrict()
			if err == nil {
				generator.fallback = true
				obs.OnFallbackActive(bizTag)
				obs.OnState(bizTag, HealthDegraded)
			}
		}
	}
	if err != nil {
		if releaseErr := generator.releaseLease(); releaseErr != nil {
			slog.Warn("release etcd lease failed", "err", releaseErr)
		}
		return nil, err
	}

	generator.workerID = workerID

	// Persist the worker slot. In fallback mode a failure is fatal (we cannot
	// afford to lose the recovery anchor); on the etcd path it is warn-only.
	if initErr := generator.cache.Ensure(); initErr != nil {
		if generator.fallback {
			return nil, initErr
		}
		slog.Warn("initialize worker cache failed", "err", initErr)
	} else if saveErr := generator.cache.Save(workerID); saveErr != nil {
		if generator.fallback {
			return nil, fmt.Errorf("persist worker slot during fallback: %w", saveErr)
		}
		slog.Warn("persist worker slot failed", "err", saveErr)
	}

	// Watermark recovery: wait out any reserved window from a previous run,
	// then set lastTimestamp to the (post-wait) wall clock — never the raw
	// watermark value, so the runtime maxBackwardMs check is unaffected.
	if err := generator.recoverWatermark(ctx); err != nil {
		return nil, err
	}

	// Start background goroutines.
	nodeCtx, cancel := context.WithCancel(context.Background())
	generator.nodeCancel = cancel
	go generator.persistWatermark(nodeCtx)
	if !localMode && !generator.fallback {
		go generator.watchLease(nodeCtx)
		go generator.refreshPermanentNode(nodeCtx)
	}

	slog.Info("snowflake tag generator ready",
		"leaf_name", cfg.LeafName,
		"biz_tag", bizTag,
		"worker_id", workerID,
		"epoch_ms", epoch,
		"local_fallback", generator.fallback)
	return generator, nil
}

// acquireEtcdSlot performs the etcd lease-grant + slot-acquire sequence.
// Context cancellation/deadline errors are returned as-is and never trigger
// the local-fallback path.
func (g *tagGenerator) acquireEtcdSlot(ctx context.Context) (int64, error) {
	if grantErr := g.lease.Grant(ctx); grantErr != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if errors.Is(grantErr, context.Canceled) || errors.Is(grantErr, context.DeadlineExceeded) {
			return 0, grantErr
		}
		slog.Warn("grant etcd lease failed", "err", grantErr)
		return 0, fmt.Errorf("%w: %w", ErrInfraUnavailable, grantErr)
	}
	slot, acquireErr := g.slots.Acquire(ctx, g.slotPrefix, g.instance, maxWorker+1)
	if acquireErr != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if errors.Is(acquireErr, context.Canceled) || errors.Is(acquireErr, context.DeadlineExceeded) {
			return 0, acquireErr
		}
		return 0, acquireErr
	}
	return int64(slot), nil
}

// acquireLocalSlot returns the cached worker slot, or worker 0 when no cache
// exists. Only used in deliberate local mode (no coordination infra at all).
func (g *tagGenerator) acquireLocalSlot() (int64, error) {
	if cached, ok := g.cache.Load(); ok && cached >= 0 && cached <= maxWorker {
		return cached, nil
	}
	return 0, nil
}

// acquireLocalSlotStrict returns the cached worker slot for the degraded
// fallback path. It NEVER falls back to slot 0: without a cached slot the
// generator refuses to guess, because multiple replicas would collide.
func (g *tagGenerator) acquireLocalSlotStrict() (int64, error) {
	if cached, ok := g.cache.Load(); ok && cached >= 0 && cached <= maxWorker {
		slog.Warn("etcd unavailable, reuse cached worker slot", "slot", cached)
		return cached, nil
	}
	return 0, ErrNoCachedSlot
}

// recoverWatermark reads the persisted time watermark and waits until the
// wall clock passes it. On success it sets lastTimestamp to the current time
// and writes an initial reserved watermark.
func (g *tagGenerator) recoverWatermark(ctx context.Context) error {
	now := g.nowFn()
	if w, ok := g.cache.LoadWatermark(); ok {
		ahead := w - now
		if ahead > g.cfg.WatermarkMaxWait.Milliseconds() {
			return fmt.Errorf("%w: watermark %d is %dms in the future (max wait %v)",
				ErrClockBackward, w, ahead, g.cfg.WatermarkMaxWait)
		}
		if ahead > 0 {
			slog.Info("waiting for persisted watermark window to pass",
				"biz_tag", g.bizTag, "watermark", w, "wait_ms", ahead)
			deadline := time.Now().Add(time.Duration(ahead) * time.Millisecond)
			for {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if g.nowFn() >= w {
					break
				}
				if time.Now().After(deadline.Add(msWaitDeadline)) {
					return fmt.Errorf("%w: watermark wait exceeded deadline", ErrClockBackward)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	g.mu.Lock()
	g.lastTimestamp = g.nowFn()
	g.mu.Unlock()
	// Write an initial reserved watermark so an immediate crash is safe.
	wm := g.nowFn() + g.cfg.WatermarkSafetyMargin.Milliseconds()
	if saveErr := g.cache.SaveWatermark(wm); saveErr != nil {
		slog.Warn("persist initial watermark failed", "err", saveErr)
	}
	return nil
}

// persistWatermark periodically writes lastTimestamp + safetyMargin so a
// crash-restart never reissues IDs from the gap window.
func (g *tagGenerator) persistWatermark(ctx context.Context) {
	ticker := time.NewTicker(g.cfg.WatermarkPersistInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.mu.Lock()
			wm := g.lastTimestamp + g.cfg.WatermarkSafetyMargin.Milliseconds()
			g.mu.Unlock()
			if err := g.cache.SaveWatermark(wm); err != nil {
				g.mu.Lock()
				g.persistFailures++
				failures := g.persistFailures
				g.mu.Unlock()
				slog.Warn("persist watermark failed", "biz_tag", g.bizTag, "err", err, "consecutive_failures", failures)
				if failures >= 3 {
					g.obs.OnState(g.bizTag, HealthDegraded)
				}
			} else {
				g.mu.Lock()
				was := g.persistFailures
				g.persistFailures = 0
				g.mu.Unlock()
				if was >= 3 && !g.fallback {
					g.obs.OnState(g.bizTag, HealthServing)
				}
			}
		}
	}
}

// next generates a single ID. The ctx parameter is checked at entry only;
// the hot path does not block on I/O.
func (g *tagGenerator) next(ctx context.Context) (int64, error) {
	if g.closed.Load() {
		return 0, ErrManagerClosed
	}
	if g.leaseLost.Load() {
		return 0, ErrLeaseLost
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nextLocked()
}

// batchNext generates count IDs in a single critical section, producing a
// monotonic exclusive batch: strictly increasing, and no concurrent caller's
// ID falls inside [first, last]. The ctx is checked every ctxCheckInterval
// iterations; all internal waits have deadlines.
func (g *tagGenerator) batchNext(ctx context.Context, count int) ([]int64, error) {
	if g.closed.Load() {
		return nil, ErrManagerClosed
	}
	if g.leaseLost.Load() {
		return nil, ErrLeaseLost
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	ids := make([]int64, count)
	for i := 0; i < count; i++ {
		if i%ctxCheckInterval == 0 && i > 0 {
			// Release the lock briefly so concurrent Next callers can make
			// progress during very large batches; then re-acquire.
			g.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			g.mu.Lock()
			if g.closed.Load() {
				return nil, ErrManagerClosed
			}
		}
		id, err := g.nextLocked()
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// nextLocked generates one ID. Caller must hold g.mu.
func (g *tagGenerator) nextLocked() (int64, error) {
	now := g.nowFn()
	if now < g.lastTimestamp {
		rollback := g.lastTimestamp - now
		if rollback <= maxBackwardMs {
			if !g.waitUntil(g.lastTimestamp, rollback) {
				g.obs.OnClockRollback(g.bizTag, rollback)
				return 0, fmt.Errorf("%w: rollback %dms did not recover", ErrClockBackward, rollback)
			}
		} else {
			g.obs.OnClockRollback(g.bizTag, rollback)
			return 0, fmt.Errorf("%w: rollback %dms", ErrClockBackward, rollback)
		}
		now = g.nowFn()
	}

	if now == g.lastTimestamp {
		g.sequence = (g.sequence + 1) & maxSeq
		if g.sequence == 0 {
			// Sequence exhausted for this millisecond: wait for the next one.
			if !g.waitUntil(g.lastTimestamp+1, 0) {
				return 0, fmt.Errorf("%w: wait for next millisecond exceeded deadline", ErrClockBackward)
			}
			now = g.nowFn()
		}
	} else {
		g.sequence = 0
	}
	g.lastTimestamp = now

	return ((now - epoch) << (workerBits + seqBits)) | (g.workerID << seqBits) | g.sequence, nil
}

// waitUntil spins until nowFn() >= target, bounded by msWaitDeadline.
// Returns false if the deadline expires. Caller must hold g.mu.
func (g *tagGenerator) waitUntil(target int64, _ int64) bool {
	deadline := time.Now().Add(msWaitDeadline)
	for g.nowFn() < target {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

func (g *tagGenerator) Close() {
	g.closeOnce.Do(func() {
		g.closed.Store(true)
		if g.nodeCancel != nil {
			g.nodeCancel()
		}
		// Persist the exact watermark so a clean restart waits zero.
		g.mu.Lock()
		wm := g.lastTimestamp
		g.mu.Unlock()
		if err := g.cache.SaveWatermark(wm); err != nil {
			slog.Warn("persist final watermark failed", "biz_tag", g.bizTag, "err", err)
		}
		if err := g.releaseLease(); err != nil {
			slog.Warn("revoke etcd lease failed, slot will expire by TTL",
				"err", err, "worker_id", g.workerID)
		}
		g.obs.OnState(g.bizTag, HealthNotServing)
		slog.Info("snowflake tag generator closed", "biz_tag", g.bizTag, "worker_id", g.workerID)
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
		g.obs.OnLeaseLost(g.bizTag)
		slog.Error("etcd lease lost, id generation is disabled",
			"biz_tag", g.bizTag, "worker_id", g.workerID)
	}
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
			err := g.slots.Refresh(updateCtx, g.slotPrefix, g.instance, g.workerID, g.nowFn())
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
