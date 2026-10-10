package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	ErrSlotOccupied      = errors.New("etcd slot occupied")
	ErrSlotsExhausted    = errors.New("etcd slots exhausted")
	ErrEtcdUnavailable   = errors.New("etcd unavailable")
	ErrSlotMappingInvalid = errors.New("etcd slot mapping invalid")
)

type slotKV interface {
	Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error)
	Txn(ctx context.Context) clientv3.Txn
}

type slotMapping struct {
	Slot      int   `json:"slot"`
	WorkerID  int   `json:"worker_id,omitempty"`
	Timestamp int64 `json:"timestamp"`
}

// SlotCoordinator coordinates lease-backed active slots with permanent identity
// mappings. All writes use etcd transactions so allocation is linearizable.
type SlotCoordinator struct {
	kv    slotKV
	lease *LeaseManager
}

func NewSlotCoordinator(client *Client, lease *LeaseManager) *SlotCoordinator {
	if client == nil || client.raw == nil {
		return newSlotCoordinator(nil, lease)
	}
	return newSlotCoordinator(client.raw, lease)
}

func newSlotCoordinator(kv slotKV, lease *LeaseManager) *SlotCoordinator {
	return &SlotCoordinator{kv: kv, lease: lease}
}

func (c *SlotCoordinator) Acquire(ctx context.Context, keyPrefix, identity string, count int) (int, error) {
	if c == nil || c.kv == nil || c.lease == nil {
		return 0, fmt.Errorf("%w: coordinator is not initialized", ErrEtcdUnavailable)
	}
	keyPrefix, identity, err := normalizeSlotArguments(keyPrefix, identity, count)
	if err != nil {
		return 0, err
	}
	if c.lease.ID() == 0 {
		return 0, fmt.Errorf("%w: lease has not been granted", ErrEtcdUnavailable)
	}

	permanentKey := permanentSlotKey(keyPrefix, identity)
	mapping, found, err := c.readMapping(ctx, permanentKey)
	if err != nil {
		return 0, err
	}
	if found {
		if mapping.Slot >= count {
			return 0, fmt.Errorf("%w: slot mapping %q slot %d is outside count %d", ErrSlotMappingInvalid, permanentKey, mapping.Slot, count)
		}
		if mapping.Timestamp > time.Now().UnixMilli() {
			return 0, fmt.Errorf("%w: slot mapping %q timestamp %d is in the future", ErrSlotMappingInvalid, permanentKey, mapping.Timestamp)
		}
		if err := c.Ensure(ctx, keyPrefix, identity, mapping.Slot); err != nil {
			return 0, err
		}
		return mapping.Slot, nil
	}

	for slot := 0; slot < count; slot++ {
		activeKey := activeSlotKey(keyPrefix, slot)
		value, err := json.Marshal(slotMapping{Slot: slot, Timestamp: time.Now().UnixMilli()})
		if err != nil {
			return 0, fmt.Errorf("marshal slot mapping: %w", err)
		}
		response, err := c.kv.Txn(ctx).
			If(
				clientv3.Compare(clientv3.CreateRevision(permanentKey), "=", 0),
				clientv3.Compare(clientv3.CreateRevision(activeKey), "=", 0),
			).
			Then(
				clientv3.OpPut(permanentKey, string(value)),
				clientv3.OpPut(activeKey, identity, clientv3.WithLease(c.lease.ID())),
			).
			Commit()
		if err != nil {
			return 0, fmt.Errorf("%w: allocate slot %d: %v", ErrEtcdUnavailable, slot, err)
		}
		if response.Succeeded {
			return slot, nil
		}

		// Another process may have created the permanent mapping while this
		// instance was scanning. Prefer that mapping instead of allocating a
		// second identity-to-slot relationship.
		mapping, found, err = c.readMapping(ctx, permanentKey)
		if err != nil {
			return 0, err
		}
		if found {
			if err := c.Ensure(ctx, keyPrefix, identity, mapping.Slot); err != nil {
				return 0, err
			}
			return mapping.Slot, nil
		}
	}
	return 0, fmt.Errorf("%w: count %d", ErrSlotsExhausted, count)
}

func (c *SlotCoordinator) Ensure(ctx context.Context, keyPrefix, identity string, slot int) error {
	if c == nil || c.kv == nil || c.lease == nil {
		return fmt.Errorf("%w: coordinator is not initialized", ErrEtcdUnavailable)
	}
	keyPrefix, identity, err := normalizeSlotArguments(keyPrefix, identity, 1)
	if err != nil {
		return err
	}
	if slot < 0 {
		return fmt.Errorf("slot must be >= 0: %d", slot)
	}
	if c.lease.ID() == 0 {
		return fmt.Errorf("%w: lease has not been granted", ErrEtcdUnavailable)
	}

	activeKey := activeSlotKey(keyPrefix, slot)
	response, err := c.kv.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(activeKey), "=", 0)).
		Then(clientv3.OpPut(activeKey, identity, clientv3.WithLease(c.lease.ID()))).
		Commit()
	if err != nil {
		return fmt.Errorf("%w: ensure slot %d: %v", ErrEtcdUnavailable, slot, err)
	}
	if !response.Succeeded {
		return fmt.Errorf("%w: slot %d", ErrSlotOccupied, slot)
	}
	return nil
}

func (c *SlotCoordinator) Refresh(ctx context.Context, keyPrefix, identity string, slot, timestamp int64) error {
	if c == nil || c.kv == nil {
		return fmt.Errorf("%w: coordinator is not initialized", ErrEtcdUnavailable)
	}
	keyPrefix, identity, err := normalizeSlotArguments(keyPrefix, identity, 1)
	if err != nil {
		return err
	}
	if slot < 0 {
		return fmt.Errorf("slot must be >= 0: %d", slot)
	}

	value, err := json.Marshal(slotMapping{Slot: int(slot), Timestamp: timestamp})
	if err != nil {
		return fmt.Errorf("marshal slot mapping: %w", err)
	}
	if _, err := c.kv.Put(ctx, permanentSlotKey(keyPrefix, identity), string(value)); err != nil {
		return fmt.Errorf("%w: refresh slot mapping: %v", ErrEtcdUnavailable, err)
	}
	return nil
}

func (c *SlotCoordinator) readMapping(ctx context.Context, key string) (slotMapping, bool, error) {
	response, err := c.kv.Get(ctx, key)
	if err != nil {
		return slotMapping{}, false, fmt.Errorf("%w: read slot mapping: %v", ErrEtcdUnavailable, err)
	}
	if len(response.Kvs) == 0 {
		return slotMapping{}, false, nil
	}

	var mapping slotMapping
	if err := json.Unmarshal(response.Kvs[0].Value, &mapping); err != nil {
		return slotMapping{}, false, fmt.Errorf("%w: invalid slot mapping %q: %w", ErrSlotMappingInvalid, key, err)
	}
	if mapping.WorkerID != 0 {
		mapping.Slot = mapping.WorkerID
	}
	if mapping.Slot < 0 {
		return slotMapping{}, false, fmt.Errorf("%w: invalid slot mapping %q: slot %d", ErrSlotMappingInvalid, key, mapping.Slot)
	}
	return mapping, true, nil
}

func normalizeSlotArguments(keyPrefix, identity string, count int) (string, string, error) {
	keyPrefix = strings.TrimSpace(keyPrefix)
	identity = strings.TrimSpace(identity)
	if keyPrefix == "" {
		return "", "", errors.New("slot key prefix cannot be empty")
	}
	if !strings.HasSuffix(keyPrefix, "/") {
		keyPrefix += "/"
	}
	if identity == "" {
		return "", "", errors.New("slot identity cannot be empty")
	}
	if count <= 0 {
		return "", "", fmt.Errorf("slot count must be > 0: %d", count)
	}
	return keyPrefix, identity, nil
}

func activeSlotKey(prefix string, slot int) string {
	return prefix + strconv.Itoa(slot)
}

func permanentSlotKey(prefix, identity string) string {
	return prefix + "forever/" + identity
}
