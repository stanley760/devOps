package idgen

import "errors"

// ErrClockBackward means clock rollback exceeded the safe threshold.
var ErrClockBackward = errors.New("clock rollback exceeded threshold, reject id generation")

// ErrLeaseLost means the lease-backed worker slot may have been taken over.
var ErrLeaseLost = errors.New("lease lost, worker slot may be occupied by another instance")

// ErrInvalidName means leaf_name or biz_tag cannot be used as an etcd/cache path component.
var ErrInvalidName = errors.New("invalid leaf_name or biz_tag")

// ErrManagerClosed means the snowflake manager has already been closed.
var ErrManagerClosed = errors.New("snowflake manager is closed")

// ErrInfraUnavailable means the worker-slot infrastructure (etcd) is not
// reachable. By default the generator fails closed (rejects ID generation);
// local fallback is only entered when Config.AllowLocalFallback is explicitly
// enabled. Context cancellation is never treated as this error.
var ErrInfraUnavailable = errors.New("worker slot infrastructure unavailable")

// ErrSlotOccupied means the requested worker slot is already held by another
// identity. Never eligible for local fallback.
var ErrSlotOccupied = errors.New("worker slot occupied")

// ErrSlotsExhausted means no worker slot is available in the configured range.
// Never eligible for local fallback.
var ErrSlotsExhausted = errors.New("worker slots exhausted")

// ErrSlotMappingInvalid means the slot mapping in etcd is corrupt or
// inconsistent. Never eligible for local fallback; requires operator attention.
var ErrSlotMappingInvalid = errors.New("worker slot mapping invalid")

// ErrNoCachedSlot means local fallback was requested but no cached worker slot
// exists on disk. The generator refuses to guess a slot (which could collide
// with another instance).
var ErrNoCachedSlot = errors.New("no cached worker slot for local fallback")

// ErrBatchTooLarge means the requested batch count exceeds the configured maximum.
var ErrBatchTooLarge = errors.New("batch count exceeds maximum")

// ErrTooManyTags means the number of live generators has reached the configured limit.
var ErrTooManyTags = errors.New("too many biz_tag generators")
