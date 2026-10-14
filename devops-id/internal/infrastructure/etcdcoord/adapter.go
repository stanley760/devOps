package etcdcoord

import (
	"context"
	"errors"
	"fmt"

	etcdinfra "github.com/stanley760/devops/devops-infra/etcd"

	iddomain "github.com/stanley760/devops/devops-id/internal/domain/idgen"
)

// mapInfraErr converts infrastructure errors to domain errors.
//
// Conversion rules:
//   - context.Canceled / context.DeadlineExceeded: passed through unchanged
//     (a cancelled request must never trigger local fallback).
//   - etcdinfra.ErrEtcdUnavailable  → iddomain.ErrInfraUnavailable  (fallback-eligible
//     only when Config.AllowLocalFallback is set).
//   - etcdinfra.ErrSlotOccupied     → iddomain.ErrSlotOccupied      (never fallback).
//   - etcdinfra.ErrSlotsExhausted   → iddomain.ErrSlotsExhausted    (never fallback).
//   - etcdinfra.ErrSlotMappingInvalid → iddomain.ErrSlotMappingInvalid (never fallback).
//   - Any other error               → iddomain.ErrSlotMappingInvalid (unknown infra
//     faults must not silently degrade).
//
// Wrapping preserves both sentinels so errors.Is matches either side.
func mapInfraErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch {
	case errors.Is(err, etcdinfra.ErrEtcdUnavailable):
		return fmt.Errorf("%w: %w", iddomain.ErrInfraUnavailable, err)
	case errors.Is(err, etcdinfra.ErrSlotOccupied):
		return fmt.Errorf("%w: %w", iddomain.ErrSlotOccupied, err)
	case errors.Is(err, etcdinfra.ErrSlotsExhausted):
		return fmt.Errorf("%w: %w", iddomain.ErrSlotsExhausted, err)
	case errors.Is(err, etcdinfra.ErrSlotMappingInvalid):
		return fmt.Errorf("%w: %w", iddomain.ErrSlotMappingInvalid, err)
	default:
		return fmt.Errorf("%w: %w", iddomain.ErrSlotMappingInvalid, err)
	}
}

// slotOps is the seam over etcdinfra.SlotCoordinator for testability.
type slotOps interface {
	Acquire(ctx context.Context, keyPrefix, identity string, count int) (int, error)
	Refresh(ctx context.Context, keyPrefix, identity string, slot, timestamp int64) error
}

// leaseOps is the seam over etcdinfra.LeaseManager for testability.
type leaseOps interface {
	Grant(ctx context.Context) error
	Lost() <-chan struct{}
	Release(ctx context.Context) error
}

// slotCoordinatorAdapter adapts etcdinfra.SlotCoordinator to the domain
// SlotCoordinator port, converting ALL infrastructure errors to domain errors.
type slotCoordinatorAdapter struct {
	coord slotOps
}

func (a slotCoordinatorAdapter) Acquire(ctx context.Context, keyPrefix, identity string, count int) (int, error) {
	slot, err := a.coord.Acquire(ctx, keyPrefix, identity, count)
	if mapped := mapInfraErr(err); mapped != nil {
		return slot, mapped
	}
	return slot, nil
}

func (a slotCoordinatorAdapter) Refresh(ctx context.Context, keyPrefix, identity string, slot, timestamp int64) error {
	return mapInfraErr(a.coord.Refresh(ctx, keyPrefix, identity, slot, timestamp))
}

// leaseAdapter wraps a LeaseLifecycle so grant/release errors are converted
// to domain errors. Lease-loss notification passes through unchanged.
type leaseAdapter struct {
	inner leaseOps
}

func (a leaseAdapter) Grant(ctx context.Context) error {
	return mapInfraErr(a.inner.Grant(ctx))
}

func (a leaseAdapter) Lost() <-chan struct{} {
	return a.inner.Lost()
}

func (a leaseAdapter) Release(ctx context.Context) error {
	return mapInfraErr(a.inner.Release(ctx))
}
