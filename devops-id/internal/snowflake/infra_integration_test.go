package snowflake

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	etcdinfra "devops-infra/etcd"
)

type fakeLeaseLifecycle struct {
	grantErr     error
	releaseErr   error
	lost         chan struct{}
	grantCalled  bool
	releaseCalls int
}

func (f *fakeLeaseLifecycle) Grant(context.Context) error {
	f.grantCalled = true
	return f.grantErr
}

func (f *fakeLeaseLifecycle) Lost() <-chan struct{} {
	return f.lost
}

func (f *fakeLeaseLifecycle) Release(context.Context) error {
	f.releaseCalls++
	return f.releaseErr
}

type fakeSlotCoordinator struct {
	acquireSlot  int
	acquireErr   error
	refreshErr   error
	acquireCalls int
	refreshCalls int
}

func (f *fakeSlotCoordinator) Acquire(context.Context, string, string, int) (int, error) {
	f.acquireCalls++
	return f.acquireSlot, f.acquireErr
}

func (f *fakeSlotCoordinator) Refresh(context.Context, string, string, int64, int64) error {
	f.refreshCalls++
	return f.refreshErr
}

func TestNewWithInfraAcquiresSlotAndCachesWorker(t *testing.T) {
	cache := t.TempDir()
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	slots := &fakeSlotCoordinator{acquireSlot: 7}

	manager, err := newManager(context.Background(), lease, slots, Config{WorkerCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if manager.workerID != 7 || !lease.grantCalled || slots.acquireCalls != 1 {
		t.Fatalf("manager worker=%d grant=%v acquire=%d", manager.workerID, lease.grantCalled, slots.acquireCalls)
	}
	data, err := os.ReadFile(filepath.Join(cache, "worker_id"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(7) {
		t.Fatalf("worker cache = %q, want 7", data)
	}
}

func TestNewWithInfraUsesLocalFallbackWhenEtcdUnavailable(t *testing.T) {
	cache := t.TempDir()
	if err := os.WriteFile(filepath.Join(cache, "worker_id"), []byte("5"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := newManager(context.Background(), nil, nil, Config{WorkerCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if manager.workerID != 5 {
		t.Fatalf("fallback worker = %d, want 5", manager.workerID)
	}
}

func TestNewWithInfraDoesNotFallbackWhenSlotOccupied(t *testing.T) {
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	slots := &fakeSlotCoordinator{acquireErr: etcdinfra.ErrSlotOccupied}

	_, err := newManager(context.Background(), lease, slots, Config{WorkerCache: t.TempDir()})
	if !errors.Is(err, etcdinfra.ErrSlotOccupied) {
		t.Fatalf("newManager() error = %v, want ErrSlotOccupied", err)
	}
	if lease.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", lease.releaseCalls)
	}
}

func TestInfraLeaseLostRejectsNewIDs(t *testing.T) {
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	manager, err := newManager(context.Background(), lease, &fakeSlotCoordinator{}, Config{WorkerCache: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	close(lease.lost)
	deadline := time.Now().Add(time.Second)
	for !manager.leaseLost.Load() {
		if time.Now().After(deadline) {
			t.Fatal("lease loss was not observed")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := manager.next(); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("next() error = %v, want ErrLeaseLost", err)
	}
}

func TestCloseWithInfraReleasesLease(t *testing.T) {
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	manager, err := newManager(context.Background(), lease, &fakeSlotCoordinator{}, Config{WorkerCache: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	manager.Close()
	if lease.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", lease.releaseCalls)
	}
}
