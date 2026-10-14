package etcdcoord

import (
	"context"
	"errors"
	"fmt"
	"testing"

	etcdinfra "github.com/stanley760/devops/devops-infra/etcd"

	iddomain "github.com/stanley760/devops/devops-id/internal/domain/idgen"
)

func TestMapInfraErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error // domain sentinel; nil means err should pass through unchanged
	}{
		{"nil", nil, nil},
		{"etcd unavailable", etcdinfra.ErrEtcdUnavailable, iddomain.ErrInfraUnavailable},
		{"etcd unavailable wrapped", fmt.Errorf("get: %w", etcdinfra.ErrEtcdUnavailable), iddomain.ErrInfraUnavailable},
		{"slot occupied", etcdinfra.ErrSlotOccupied, iddomain.ErrSlotOccupied},
		{"slots exhausted", etcdinfra.ErrSlotsExhausted, iddomain.ErrSlotsExhausted},
		{"slot mapping invalid", etcdinfra.ErrSlotMappingInvalid, iddomain.ErrSlotMappingInvalid},
		{"context canceled", context.Canceled, nil},         // pass through
		{"context deadline", context.DeadlineExceeded, nil}, // pass through
		{"unknown error", errors.New("boom"), iddomain.ErrSlotMappingInvalid},
		{"wrapped unknown", fmt.Errorf("op: %w", errors.New("boom")), iddomain.ErrSlotMappingInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapInfraErr(tt.err)
			if tt.err == nil {
				if got != nil {
					t.Fatalf("mapInfraErr(nil) = %v, want nil", got)
				}
				return
			}
			if tt.want == nil {
				// Should pass through unchanged
				if !errors.Is(got, tt.err) {
					t.Fatalf("mapInfraErr(%v) = %v, want pass-through", tt.err, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("mapInfraErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type fakeSlotOps struct {
	acquireErr  error
	refreshErr  error
	acquireSlot int
}

func (f *fakeSlotOps) Acquire(context.Context, string, string, int) (int, error) {
	return f.acquireSlot, f.acquireErr
}

func (f *fakeSlotOps) Refresh(context.Context, string, string, int64, int64) error {
	return f.refreshErr
}

type fakeLeaseOps struct {
	grantErr error
	lost     chan struct{}
}

func (f *fakeLeaseOps) Grant(context.Context) error   { return f.grantErr }
func (f *fakeLeaseOps) Lost() <-chan struct{}         { return f.lost }
func (f *fakeLeaseOps) Release(context.Context) error { return nil }

func TestSlotAdapterAcquireMapsErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"etcd unavailable", etcdinfra.ErrEtcdUnavailable, iddomain.ErrInfraUnavailable},
		{"slot occupied", etcdinfra.ErrSlotOccupied, iddomain.ErrSlotOccupied},
		{"slots exhausted", etcdinfra.ErrSlotsExhausted, iddomain.ErrSlotsExhausted},
		{"context canceled", context.Canceled, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := slotCoordinatorAdapter{coord: &fakeSlotOps{acquireErr: tt.err}}
			_, err := adapter.Acquire(context.Background(), "prefix", "id", 10)
			if tt.want == nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("Acquire() error = %v, want pass-through %v", err, tt.err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Acquire() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSlotAdapterRefreshMapsErrors(t *testing.T) {
	adapter := slotCoordinatorAdapter{coord: &fakeSlotOps{refreshErr: etcdinfra.ErrEtcdUnavailable}}
	err := adapter.Refresh(context.Background(), "prefix", "id", 1, 0)
	if !errors.Is(err, iddomain.ErrInfraUnavailable) {
		t.Fatalf("Refresh() error = %v, want ErrInfraUnavailable", err)
	}
}

func TestLeaseAdapterGrantMapsErrors(t *testing.T) {
	adapter := leaseAdapter{inner: &fakeLeaseOps{grantErr: etcdinfra.ErrEtcdUnavailable, lost: make(chan struct{})}}
	err := adapter.Grant(context.Background())
	if !errors.Is(err, iddomain.ErrInfraUnavailable) {
		t.Fatalf("Grant() error = %v, want ErrInfraUnavailable", err)
	}
}

func TestLeaseAdapterGrantContextPassthrough(t *testing.T) {
	adapter := leaseAdapter{inner: &fakeLeaseOps{grantErr: context.Canceled, lost: make(chan struct{})}}
	err := adapter.Grant(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Grant() error = %v, want context.Canceled", err)
	}
}
