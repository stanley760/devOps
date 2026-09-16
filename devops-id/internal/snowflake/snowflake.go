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

	slotPrefix = "idgen/snowflake/"

	// maxBackwardMs is the tolerated clock rollback threshold.
	maxBackwardMs = int64(5)
)

// ErrClockBackward means clock rollback exceeded the safe threshold.
var ErrClockBackward = errors.New("clock rollback exceeded threshold, reject id generation")

// ErrLeaseLost means the lease-backed worker slot may have been taken over.
var ErrLeaseLost = errors.New("lease lost, worker slot may be occupied by another instance")

// Config contains snowflake-specific configuration.
type Config struct {
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

// Manager generates snowflake IDs.
type Manager struct {
	mu            sync.Mutex
	lastTimestamp int64
	sequence      int64
	workerID      int64

	lease      leaseLifecycle
	slots      slotCoordinator
	nodeCancel context.CancelFunc
	leaseLost  atomic.Bool

	workerFile string
	instance   string
	closeOnce  sync.Once
}

// New creates a manager and acquires a lease-backed worker slot.
func New(ctx context.Context, client *etcdinfra.Client, cfg Config) (*Manager, error) {
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 10 * time.Second
	}
	if client == nil {
		return newManager(ctx, nil, nil, cfg)
	}

	lease := etcdinfra.NewLeaseManager(client, cfg.LeaseTTL)
	slots := etcdinfra.NewSlotCoordinator(client, lease)
	return newManager(ctx, lease, slots, cfg)
}

func newManager(ctx context.Context, lease leaseLifecycle, slots slotCoordinator, cfg Config) (*Manager, error) {
	cacheDir := cfg.WorkerCache
	if cacheDir == "" {
		cacheDir = "data"
	}
	manager := &Manager{
		lease:      lease,
		slots:      slots,
		workerFile: filepath.Join(cacheDir, "worker_id"),
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
		slot, acquireErr := slots.Acquire(ctx, slotPrefix, manager.instance, maxWorker+1)
		workerID = int64(slot)
		err = acquireErr
	}

	if errors.Is(err, etcdinfra.ErrEtcdUnavailable) {
		usedLocalFallback = true
		if releaseErr := manager.releaseLease(); releaseErr != nil {
			slog.Warn("release etcd lease after fallback failed", "err", releaseErr)
		}
		if initErr := manager.initLocalCache(); initErr != nil {
			return nil, initErr
		}
		workerID, err = manager.acquireLocalSlot()
	}
	if err != nil {
		if releaseErr := manager.releaseLease(); releaseErr != nil {
			slog.Warn("release etcd lease failed", "err", releaseErr)
		}
		return nil, err
	}

	manager.workerID = workerID
	manager.lastTimestamp = time.Now().UnixMilli()
	if err := manager.initLocalCache(); err != nil {
		slog.Warn("initialize worker cache failed", "err", err)
	} else {
		writeIntFile(manager.workerFile, workerID)
	}

	if !usedLocalFallback {
		nodeCtx, cancel := context.WithCancel(context.Background())
		manager.nodeCancel = cancel
		go manager.watchLease(nodeCtx)
		go manager.refreshPermanentNode(nodeCtx)
	}

	slog.Info("snowflake manager ready",
		"worker_id", workerID, "epoch_ms", epoch, "local_fallback", usedLocalFallback)
	return manager, nil
}

// Next generates one ID. bizTag is ignored because snowflake is global.
func (m *Manager) Next(ctx context.Context, bizTag string) (int64, error) {
	return m.next()
}

// BatchNext generates strictly increasing IDs.
func (m *Manager) BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error) {
	if count < 1 {
		return nil, fmt.Errorf("count must be >= 1: %d", count)
	}
	ids := make([]int64, count)
	for i := 0; i < count; i++ {
		id, err := m.next()
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func (m *Manager) next() (int64, error) {
	if m.leaseLost.Load() {
		return 0, ErrLeaseLost
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()
	if now < m.lastTimestamp {
		rollback := m.lastTimestamp - now
		if rollback <= maxBackwardMs {
			deadline := time.Now().Add(time.Duration(rollback)*time.Millisecond + time.Second)
			for now < m.lastTimestamp {
				if time.Now().After(deadline) {
					return 0, fmt.Errorf("%w: rollback %dms did not recover", ErrClockBackward, rollback)
				}
				now = time.Now().UnixMilli()
			}
		} else {
			return 0, fmt.Errorf("%w: rollback %dms", ErrClockBackward, rollback)
		}
	}

	if now == m.lastTimestamp {
		m.sequence = (m.sequence + 1) & maxSeq
		if m.sequence == 0 {
			for now <= m.lastTimestamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		m.sequence = 0
	}
	m.lastTimestamp = now

	return ((now - epoch) << (workerBits + seqBits)) | (m.workerID << seqBits) | m.sequence, nil
}

// Close cancels refreshes and revokes the lease immediately.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		if m.nodeCancel != nil {
			m.nodeCancel()
		}
		if err := m.releaseLease(); err != nil {
			slog.Warn("revoke etcd lease failed, slot will expire by TTL", "err", err)
		}
		slog.Info("snowflake manager closed", "worker_id", m.workerID)
	})
}

func (m *Manager) releaseLease() error {
	if m.lease == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.lease.Release(ctx)
}

func (m *Manager) watchLease(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-m.lease.Lost():
		m.leaseLost.Store(true)
		slog.Error("etcd lease lost, id generation is disabled", "worker_id", m.workerID)
	}
}

func (m *Manager) acquireLocalSlot() (int64, error) {
	if cached, ok := readIntFile(m.workerFile); ok && cached >= 0 && cached <= maxWorker {
		slog.Warn("etcd unavailable, reuse cached worker slot", "slot", cached)
		return cached, nil
	}
	writeIntFile(m.workerFile, 0)
	slog.Warn("etcd unavailable and no cached slot, use worker slot 0")
	return 0, nil
}

func (m *Manager) initLocalCache() error {
	dir := filepath.Dir(m.workerFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory %s: %w", dir, err)
	}
	return nil
}

func (m *Manager) refreshPermanentNode(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := m.slots.Refresh(updateCtx, slotPrefix, m.instance, m.workerID, time.Now().UnixMilli())
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
