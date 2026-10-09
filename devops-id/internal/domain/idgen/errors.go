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
// reachable. The generator falls back to the locally cached worker slot
// instead of failing hard; slot-occupied errors are NOT wrapped in this.
var ErrInfraUnavailable = errors.New("worker slot infrastructure unavailable")
