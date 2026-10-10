// 发号域单元测试：位布局、单调性、时钟回拨保护（无需 etcd）。
// etcd 槽位抢占/lease 释放为运行时验收项（双实例启动槽位不冲突、kill 后 10s 可抢）。
package idgen

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestGenerator(workerID int64) *tagGenerator {
	return &tagGenerator{
		workerID:      workerID,
		lastTimestamp: time.Now().UnixMilli(),
		nowFn:         func() int64 { return time.Now().UnixMilli() },
		obs:           noopObserver{},
		cfg: Config{
			WatermarkSafetyMargin:    5 * time.Second,
			WatermarkPersistInterval: 3 * time.Second,
			WatermarkMaxWait:         30 * time.Second,
		},
	}
}

func TestNextBitLayoutAndMonotonic(t *testing.T) {
	const worker = int64(777)
	generator := newTestGenerator(worker)

	var prev int64
	for i := 0; i < 1000; i++ {
		id, err := generator.next(context.Background())
		if err != nil {
			t.Fatalf("第 %d 次发号失败: %v", i, err)
		}
		if id <= prev {
			t.Fatalf("ID 未递增: %d <= %d", id, prev)
		}
		if got := (id >> seqBits) & maxWorker; got != worker {
			t.Fatalf("worker 位错误: got %d want %d", got, worker)
		}
		if got := id & maxSeq; got < 0 || got > maxSeq {
			t.Fatalf("序列位越界: %d", got)
		}
		if ms := (id >> (workerBits + seqBits)) + epoch; ms > time.Now().UnixMilli() {
			t.Fatalf("时间戳位超前当前时间: %d", ms)
		}
		prev = id
	}
}

func TestBatchNextStrictlyIncreasing(t *testing.T) {
	manager, err := NewManager(Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := manager.BatchNext(context.Background(), "any", 500)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("批量 ID 未严格递增: ids[%d]=%d <= ids[%d]=%d", i, ids[i], i-1, ids[i-1])
		}
	}
}

func TestClockBackwardSmallSpin(t *testing.T) {
	generator := newTestGenerator(1)
	generator.lastTimestamp = time.Now().UnixMilli() + 3 // 回拨 3ms（< 5ms 阈值）
	if _, err := generator.next(context.Background()); err != nil {
		t.Fatalf("小回拨应自旋等待追平，got %v", err)
	}
}

func TestClockBackwardLargeReject(t *testing.T) {
	generator := newTestGenerator(1)
	generator.lastTimestamp = time.Now().UnixMilli() + 100 // 回拨 100ms（≥ 5ms 阈值）
	if _, err := generator.next(context.Background()); !errors.Is(err, ErrClockBackward) {
		t.Fatalf("大回拨应拒绝发号，got %v", err)
	}
}

func TestLeaseLostReject(t *testing.T) {
	// lease 失效（槽位可能被他实例抢占）后必须熔断发号
	generator := newTestGenerator(1)
	generator.leaseLost.Store(true)
	if _, err := generator.next(context.Background()); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("lease 失效应拒绝发号，got %v", err)
	}
}
