package etcd

import (
	"context"
	"errors"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type fakeLeaseClient struct {
	grantErr     error
	keepAliveErr error
	revokeErr    error
	grantedTTL   int64
	revokedID    clientv3.LeaseID
	keepAliveCh  chan *clientv3.LeaseKeepAliveResponse
}

func (f *fakeLeaseClient) Grant(_ context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
	f.grantedTTL = ttl
	if f.grantErr != nil {
		return nil, f.grantErr
	}
	return &clientv3.LeaseGrantResponse{ID: 42}, nil
}

func (f *fakeLeaseClient) KeepAlive(_ context.Context, _ clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	if f.keepAliveErr != nil {
		return nil, f.keepAliveErr
	}
	return f.keepAliveCh, nil
}

func (f *fakeLeaseClient) Revoke(_ context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
	f.revokedID = id
	return &clientv3.LeaseRevokeResponse{}, f.revokeErr
}

func TestNewLeaseManagerDefaultsTTL(t *testing.T) {
	manager := NewLeaseManager(&Client{}, 0)
	if manager.ttl != 10*time.Second {
		t.Fatalf("ttl = %v, want 10s", manager.ttl)
	}
}

func TestLeaseManagerGrantError(t *testing.T) {
	fake := &fakeLeaseClient{grantErr: errors.New("grant failed")}
	manager := newLeaseManagerForClient(fake, time.Second)

	err := manager.Grant(context.Background())
	if !errors.Is(err, fake.grantErr) {
		t.Fatalf("Grant() error = %v, want %v", err, fake.grantErr)
	}
	if manager.ID() != 0 {
		t.Fatalf("ID() = %d, want 0 after grant failure", manager.ID())
	}
}

func TestLeaseManagerGrantKeepsAliveAndSignalsLoss(t *testing.T) {
	fake := &fakeLeaseClient{keepAliveCh: make(chan *clientv3.LeaseKeepAliveResponse, 1)}
	manager := newLeaseManagerForClient(fake, 2*time.Second)

	ctx := context.Background()
	if err := manager.Grant(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.grantedTTL != 2 {
		t.Fatalf("grantedTTL = %d, want 2", fake.grantedTTL)
	}
	if manager.ID() != 42 {
		t.Fatalf("ID() = %d, want 42", manager.ID())
	}

	select {
	case fake.keepAliveCh <- &clientv3.LeaseKeepAliveResponse{}:
	default:
		t.Fatal("keepalive channel should remain drained")
	}
	close(fake.keepAliveCh)

	select {
	case <-manager.Lost():
	case <-time.After(time.Second):
		t.Fatal("lease loss was not signaled")
	}
}

func TestLeaseManagerReleaseRevokesAndSuppressesLoss(t *testing.T) {
	fake := &fakeLeaseClient{keepAliveCh: make(chan *clientv3.LeaseKeepAliveResponse)}
	manager := newLeaseManagerForClient(fake, time.Second)
	if err := manager.Grant(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := manager.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.revokedID != 42 {
		t.Fatalf("revokedID = %d, want 42", fake.revokedID)
	}

	select {
	case <-manager.Lost():
		t.Fatal("normal shutdown should not signal lease loss")
	default:
	}
}
