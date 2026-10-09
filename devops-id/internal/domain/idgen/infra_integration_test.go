package idgen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	etcdinfra "devops-infra/etcd"

	"devops-id/internal/infrastructure/workerfile"
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
	prefixes     []string
}

func (f *fakeSlotCoordinator) Acquire(_ context.Context, keyPrefix string, _ string, _ int) (int, error) {
	f.acquireCalls++
	f.prefixes = append(f.prefixes, keyPrefix)
	return f.acquireSlot, f.acquireErr
}

func (f *fakeSlotCoordinator) Refresh(context.Context, string, string, int64, int64) error {
	f.refreshCalls++
	return f.refreshErr
}

func workerOf(id int64) int64 {
	return (id >> seqBits) & maxWorker
}

func TestManagerAcquiresSlotAndCachesWorker(t *testing.T) {
	cache := t.TempDir()
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	slots := &fakeSlotCoordinator{acquireSlot: 7}

	manager, err := NewManager(Config{LeafName: "leaf"}, func(bizTag string) TagInfra {
		return TagInfra{Lease: lease, Slots: slots, Cache: workerfile.New(cache, "leaf", bizTag)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	id, err := manager.Next(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if workerOf(id) != 7 || !lease.grantCalled || slots.acquireCalls != 1 {
		t.Fatalf("worker=%d grant=%v acquire=%d", workerOf(id), lease.grantCalled, slots.acquireCalls)
	}
	if len(slots.prefixes) != 1 || slots.prefixes[0] != "idgen/snowflake/leaf/user/" {
		t.Fatalf("slot prefixes = %v", slots.prefixes)
	}
	data, err := os.ReadFile(filepath.Join(cache, "leaf", "user", "worker_id"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(7) {
		t.Fatalf("worker cache = %q, want 7", data)
	}
}

func TestManagerUsesIndependentWorkerSpacePerBizTag(t *testing.T) {
	cache := t.TempDir()
	var leases []*fakeLeaseLifecycle
	var slots []*fakeSlotCoordinator
	call := 0

	manager, err := NewManager(Config{LeafName: "leaf"}, func(bizTag string) TagInfra {
		call++
		lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
		coordinator := &fakeSlotCoordinator{acquireSlot: 6 + call}
		leases = append(leases, lease)
		slots = append(slots, coordinator)
		return TagInfra{Lease: lease, Slots: coordinator, Cache: workerfile.New(cache, "leaf", bizTag)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	userID, err := manager.Next(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	roleID, err := manager.Next(context.Background(), "role")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Next(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}

	if workerOf(userID) != 7 || workerOf(roleID) != 8 {
		t.Fatalf("workers = (%d, %d), want (7, 8)", workerOf(userID), workerOf(roleID))
	}
	wantPrefixes := []string{
		"idgen/snowflake/leaf/user/",
		"idgen/snowflake/leaf/role/",
	}
	if len(slots) != len(wantPrefixes) {
		t.Fatalf("generator count = %d, want %d", len(slots), len(wantPrefixes))
	}
	for i, want := range wantPrefixes {
		if slots[i].acquireCalls != 1 || slots[i].prefixes[0] != want {
			t.Fatalf("slot[%d] calls=%d prefixes=%v, want %q", i, slots[i].acquireCalls, slots[i].prefixes, want)
		}
	}

	userCache := filepath.Join(cache, "leaf", "user", "worker_id")
	roleCache := filepath.Join(cache, "leaf", "role", "worker_id")
	for path, want := range map[string]string{userCache: "7", roleCache: "8"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("worker cache %s = %q, want %q", path, data, want)
		}
	}

	manager.Close()
	for i, lease := range leases {
		if lease.releaseCalls != 1 {
			t.Fatalf("lease[%d] release calls = %d, want 1", i, lease.releaseCalls)
		}
	}
}

func TestManagerRejectsInvalidBizTag(t *testing.T) {
	manager, err := NewManager(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if _, err := manager.Next(context.Background(), "user/role"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Next() error = %v, want ErrInvalidName", err)
	}
}

func TestManagerClosedRejectsNewRequests(t *testing.T) {
	manager, err := NewManager(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()

	if _, err := manager.Next(context.Background(), "user"); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("Next() error = %v, want ErrManagerClosed", err)
	}
}

func TestManagerUsesLocalFallbackPerBizTag(t *testing.T) {
	cache := t.TempDir()
	workerFile := filepath.Join(cache, "leaf", "user", "worker_id")
	if err := os.MkdirAll(filepath.Dir(workerFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workerFile, []byte("5"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(Config{LeafName: "leaf"}, func(bizTag string) TagInfra {
		return TagInfra{Cache: workerfile.New(cache, "leaf", bizTag)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	id, err := manager.Next(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if workerOf(id) != 5 {
		t.Fatalf("fallback worker = %d, want 5", workerOf(id))
	}
}

func TestManagerDoesNotFallbackWhenSlotOccupied(t *testing.T) {
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	slots := &fakeSlotCoordinator{acquireErr: etcdinfra.ErrSlotOccupied}

	manager, err := NewManager(Config{}, func(bizTag string) TagInfra {
		return TagInfra{Lease: lease, Slots: slots, Cache: workerfile.New(t.TempDir(), "devops-id", bizTag)}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Next(context.Background(), "user"); !errors.Is(err, etcdinfra.ErrSlotOccupied) {
		t.Fatalf("Next() error = %v, want ErrSlotOccupied", err)
	}
	if lease.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", lease.releaseCalls)
	}
}

func TestInfraLeaseLostRejectsNewIDs(t *testing.T) {
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	manager, err := NewManager(Config{}, func(bizTag string) TagInfra {
		return TagInfra{Lease: lease, Slots: &fakeSlotCoordinator{}, Cache: workerfile.New(t.TempDir(), "devops-id", bizTag)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	if _, err := manager.Next(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}
	close(lease.lost)
	deadline := time.Now().Add(time.Second)
	for {
		_, err := manager.Next(context.Background(), "user")
		if errors.Is(err, ErrLeaseLost) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Next() error = %v, want ErrLeaseLost", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCloseWithInfraReleasesLease(t *testing.T) {
	lease := &fakeLeaseLifecycle{lost: make(chan struct{})}
	manager, err := NewManager(Config{}, func(bizTag string) TagInfra {
		return TagInfra{Lease: lease, Slots: &fakeSlotCoordinator{}, Cache: workerfile.New(t.TempDir(), "devops-id", bizTag)}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Next(context.Background(), "user"); err != nil {
		t.Fatal(err)
	}

	manager.Close()
	if lease.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", lease.releaseCalls)
	}
}
