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
	closeOnce  sync.Once
}

func newTagGenerator(
	ctx context.Context,
	infra TagInfra,
	cfg Config,
	bizTag string,
) (*tagGenerator, error) {
	if err := validateName(bizTag); err != nil {
		return nil, fmt.Errorf("invalid biz_tag: %w", err)
	}

	generator := &tagGenerator{
		lease:      infra.Lease,
		slots:      infra.Slots,
		cache:      infra.Cache,
		slotPrefix: strings.Join([]string{slotRoot, cfg.LeafName, bizTag}, "/") + "/",
		instance:   instanceID(),
	}

	var workerID int64
	var err error
	usedLocalFallback := false
	if generator.lease == nil || generator.slots == nil {
		err = ErrInfraUnavailable
	} else if grantErr := generator.lease.Grant(ctx); grantErr != nil {
		slog.Warn("grant etcd lease failed, using local worker fallback", "err", grantErr)
		err = ErrInfraUnavailable
	} else {
		slot, acquireErr := generator.slots.Acquire(ctx, generator.slotPrefix, generator.instance, maxWorker+1)
		workerID = int64(slot)
		err = acquireErr
	}

	if errors.Is(err, ErrInfraUnavailable) {
		usedLocalFallback = true
		if releaseErr := generator.releaseLease(); releaseErr != nil {
			slog.Warn("release etcd lease after fallback failed", "err", releaseErr)
		}
		if initErr := generator.cache.Ensure(); initErr != nil {
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
	if err := generator.cache.Ensure(); err != nil {
		slog.Warn("initialize worker cache failed", "err", err)
	} else {
		generator.cache.Save(workerID)
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
	if cached, ok := g.cache.Load(); ok && cached >= 0 && cached <= maxWorker {
		slog.Warn("etcd unavailable, reuse cached worker slot", "slot", cached)
		return cached, nil
	}
	g.cache.Save(0)
	slog.Warn("etcd unavailable and no cached slot, use worker slot 0")
	return 0, nil
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
