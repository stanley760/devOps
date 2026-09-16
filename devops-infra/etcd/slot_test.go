package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	mvccpb "go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type fakeSlotKV struct {
	values     map[string]string
	txnResults map[string]bool
	txns       []*fakeTxn
	puts       map[string]string
}

func (f *fakeSlotKV) Get(_ context.Context, key string, _ ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	value, exists := f.values[key]
	if !exists {
		return &clientv3.GetResponse{}, nil
	}
	return &clientv3.GetResponse{
		Kvs: []*mvccpb.KeyValue{{Key: []byte(key), Value: []byte(value)}},
	}, nil
}

func (f *fakeSlotKV) Txn(_ context.Context) clientv3.Txn {
	txn := &fakeTxn{kv: f}
	f.txns = append(f.txns, txn)
	return txn
}

func (f *fakeSlotKV) Put(_ context.Context, key, value string, _ ...clientv3.OpOption) (*clientv3.PutResponse, error) {
	f.puts[key] = value
	return &clientv3.PutResponse{}, nil
}

type fakeTxn struct {
	kv      *fakeSlotKV
	cmps    []clientv3.Cmp
	thenOps []clientv3.Op
}

func (t *fakeTxn) If(cmps ...clientv3.Cmp) clientv3.Txn {
	t.cmps = append(t.cmps, cmps...)
	return t
}

func (t *fakeTxn) Then(ops ...clientv3.Op) clientv3.Txn {
	t.thenOps = append(t.thenOps, ops...)
	return t
}

func (t *fakeTxn) Else(_ ...clientv3.Op) clientv3.Txn {
	return t
}

func (t *fakeTxn) Commit() (*clientv3.TxnResponse, error) {
	if len(t.thenOps) == 0 {
		return nil, errors.New("transaction has no operations")
	}
	activeKey := string(t.thenOps[len(t.thenOps)-1].KeyBytes())
	return &clientv3.TxnResponse{Succeeded: t.kv.txnResults[activeKey]}, nil
}

func newSlotTestCoordinator(kv *fakeSlotKV, leaseID clientv3.LeaseID) *SlotCoordinator {
	return newSlotCoordinator(kv, &LeaseManager{id: leaseID})
}

func TestSlotCoordinatorEnsureUsesLeaseAndCAS(t *testing.T) {
	kv := &fakeSlotKV{values: map[string]string{}, txnResults: map[string]bool{}}
	kv.txnResults["idgen/snowflake/7"] = true
	coordinator := newSlotTestCoordinator(kv, 42)

	if err := coordinator.Ensure(context.Background(), "idgen/snowflake/", "host-a", 7); err != nil {
		t.Fatal(err)
	}
	if len(kv.txns) != 1 {
		t.Fatalf("transaction count = %d, want 1", len(kv.txns))
	}
	txn := kv.txns[0]
	if len(txn.cmps) != 1 {
		t.Fatalf("comparison count = %d, want 1", len(txn.cmps))
	}
	if len(txn.thenOps) != 1 || string(txn.thenOps[0].KeyBytes()) != "idgen/snowflake/7" {
		t.Fatalf("unexpected transaction operations: %+v", txn.thenOps)
	}
	if value := string(txn.thenOps[0].ValueBytes()); value != "host-a" {
		t.Fatalf("slot value = %q, want host-a", value)
	}
}

func TestSlotCoordinatorAcquireReusesPermanentMapping(t *testing.T) {
	kv := &fakeSlotKV{
		values: map[string]string{
			"idgen/snowflake/forever/host-a": `{"worker_id":7,"timestamp":123}`,
		},
		txnResults: map[string]bool{"idgen/snowflake/7": true},
	}
	coordinator := newSlotTestCoordinator(kv, 42)

	slot, err := coordinator.Acquire(context.Background(), "idgen/snowflake/", "host-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if slot != 7 {
		t.Fatalf("slot = %d, want 7", slot)
	}
	if len(kv.txns) != 1 || len(kv.txns[0].thenOps) != 1 {
		t.Fatalf("expected one fixed-slot transaction, got %+v", kv.txns)
	}
}

func TestSlotCoordinatorAcquireAllocatesFirstFreeSlot(t *testing.T) {
	kv := &fakeSlotKV{
		values:     map[string]string{},
		txnResults: map[string]bool{"idgen/snowflake/0": false, "idgen/snowflake/1": true},
	}
	coordinator := newSlotTestCoordinator(kv, 42)

	slot, err := coordinator.Acquire(context.Background(), "idgen/snowflake/", "host-a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if slot != 1 {
		t.Fatalf("slot = %d, want 1", slot)
	}
	if len(kv.txns) != 2 {
		t.Fatalf("transaction count = %d, want 2", len(kv.txns))
	}

	var mapping slotMapping
	if err := json.Unmarshal(kv.txns[1].thenOps[0].ValueBytes(), &mapping); err != nil {
		t.Fatal(err)
	}
	if mapping.Slot != 1 {
		t.Fatalf("permanent mapping slot = %d, want 1", mapping.Slot)
	}
}

func TestSlotCoordinatorAcquireRejectsOccupiedPermanentSlot(t *testing.T) {
	kv := &fakeSlotKV{
		values: map[string]string{
			"idgen/snowflake/forever/host-a": `{"slot":7,"timestamp":123}`,
		},
		txnResults: map[string]bool{"idgen/snowflake/7": false},
	}
	coordinator := newSlotTestCoordinator(kv, 42)

	_, err := coordinator.Acquire(context.Background(), "idgen/snowflake/", "host-a", 10)
	if !errors.Is(err, ErrSlotOccupied) {
		t.Fatalf("Acquire() error = %v, want ErrSlotOccupied", err)
	}
}

func TestSlotCoordinatorAcquireRejectsFutureMappingTimestamp(t *testing.T) {
	kv := &fakeSlotKV{
		values: map[string]string{
			"idgen/snowflake/forever/host-a": `{"slot":7,"timestamp":99999999999999}`,
		},
		txnResults: map[string]bool{"idgen/snowflake/7": true},
	}
	coordinator := newSlotTestCoordinator(kv, 42)

	_, err := coordinator.Acquire(context.Background(), "idgen/snowflake/", "host-a", 10)
	if err == nil {
		t.Fatal("expected future mapping timestamp to be rejected")
	}
}

func TestSlotCoordinatorRefreshUpdatesPermanentMapping(t *testing.T) {
	kv := &fakeSlotKV{puts: map[string]string{}}
	coordinator := newSlotTestCoordinator(kv, 42)

	if err := coordinator.Refresh(context.Background(), "idgen/snowflake/", "host-a", 7, 123456); err != nil {
		t.Fatal(err)
	}
	value := kv.puts["idgen/snowflake/forever/host-a"]
	var mapping slotMapping
	if err := json.Unmarshal([]byte(value), &mapping); err != nil {
		t.Fatal(err)
	}
	if mapping.Slot != 7 || mapping.Timestamp != 123456 {
		t.Fatalf("refresh mapping = %+v, want slot 7 and timestamp 123456", mapping)
	}
}

func TestSlotCoordinatorAcquireRejectsFullSlots(t *testing.T) {
	kv := &fakeSlotKV{
		values:     map[string]string{},
		txnResults: map[string]bool{"idgen/snowflake/0": false, "idgen/snowflake/1": false},
	}
	coordinator := newSlotTestCoordinator(kv, 42)

	_, err := coordinator.Acquire(context.Background(), "idgen/snowflake/", "host-a", 2)
	if !errors.Is(err, ErrSlotsExhausted) {
		t.Fatalf("Acquire() error = %v, want ErrSlotsExhausted", err)
	}
}

func TestSlotCoordinatorRejectsInvalidArguments(t *testing.T) {
	coordinator := newSlotTestCoordinator(&fakeSlotKV{}, 42)
	tests := []struct {
		name     string
		prefix   string
		identity string
		count    int
	}{
		{"empty prefix", "", "host", 1},
		{"empty identity", "idgen/", "", 1},
		{"zero count", "idgen/", "host", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := coordinator.Acquire(context.Background(), tt.prefix, tt.identity, tt.count); err == nil {
				t.Fatal("expected invalid argument error")
			}
		})
	}
}
