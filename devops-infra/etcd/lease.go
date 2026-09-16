package etcd

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

var ErrLeaseLost = errors.New("etcd lease lost")

type leaseClient interface {
	Grant(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error)
	KeepAlive(ctx context.Context, id clientv3.LeaseID) (<-chan *clientv3.LeaseKeepAliveResponse, error)
	Revoke(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error)
}

// LeaseManager owns the full lifecycle of one etcd lease. Keepalive responses
// are drained so the client's bounded queue cannot fill, and channel closure
// is translated into a single lost signal.
type LeaseManager struct {
	client leaseClient
	ttl    time.Duration

	mu              sync.Mutex
	id              clientv3.LeaseID
	keepAliveCancel context.CancelFunc

	lost           chan struct{}
	lostClose      sync.Once
	normalShutdown atomic.Bool
	release        sync.Once
}

func NewLeaseManager(client *Client, ttl time.Duration) *LeaseManager {
	if client != nil && client.raw != nil {
		return newLeaseManagerForClient(client.raw, ttl)
	}
	return newLeaseManagerForClient(nil, ttl)
}

func newLeaseManagerForClient(client leaseClient, ttl time.Duration) *LeaseManager {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	return &LeaseManager{
		client: client,
		ttl:    ttl,
		lost:   make(chan struct{}),
	}
}

func (m *LeaseManager) Grant(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil {
		return errors.New("etcd client is nil")
	}
	if m.id != 0 {
		return errors.New("lease has already been granted")
	}

	ttlSeconds := int64(math.Ceil(m.ttl.Seconds()))
	response, err := m.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return err
	}
	m.id = response.ID

	keepAliveCtx, cancel := context.WithCancel(context.Background())
	keepAliveCh, err := m.client.KeepAlive(keepAliveCtx, m.id)
	if err != nil {
		cancel()
		_, _ = m.client.Revoke(ctx, m.id)
		m.id = 0
		return err
	}
	m.keepAliveCancel = cancel
	go m.watch(keepAliveCh)
	return nil
}

func (m *LeaseManager) ID() clientv3.LeaseID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.id
}

func (m *LeaseManager) Lost() <-chan struct{} {
	return m.lost
}

func (m *LeaseManager) Release(ctx context.Context) error {
	var err error
	m.release.Do(func() {
		m.normalShutdown.Store(true)

		m.mu.Lock()
		cancel := m.keepAliveCancel
		id := m.id
		client := m.client
		m.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		if client != nil && id != 0 {
			_, err = client.Revoke(ctx, id)
		}
	})
	return err
}

func (m *LeaseManager) watch(keepAliveCh <-chan *clientv3.LeaseKeepAliveResponse) {
	for range keepAliveCh {
	}
	if !m.normalShutdown.Load() {
		m.lostClose.Do(func() { close(m.lost) })
	}
}
