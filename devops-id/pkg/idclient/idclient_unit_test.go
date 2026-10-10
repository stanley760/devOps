package idclient

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient creates a Client with a stubbed fetchFn (no gRPC).
func newTestClient(batch int, fetchFn func(ctx context.Context, bizTag string) ([]int64, error)) *Client {
	c := &Client{
		cfg:     BufferConfig{Batch: batch, LowWater: 0.2},
		buffers: make(map[string]*tagBuffer),
		fetchFn: fetchFn,
	}
	c.initMetrics()
	return c
}

func TestSingleFlightOneRPCPerTag(t *testing.T) {
	// P1-3: 并发 Next 空缓冲时只发一次 RPC
	var calls atomic.Int32
	start := make(chan struct{})
	c := newTestClient(100, func(ctx context.Context, bizTag string) ([]int64, error) {
		calls.Add(1)
		<-start // 所有 goroutine 到达后才返回
		ids := make([]int64, 100)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		return ids, nil
	})

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Next(context.Background(), "tag")
		}()
	}
	time.Sleep(50 * time.Millisecond) // 等所有 goroutine 进入
	close(start)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1 (single-flight)", got)
	}
}

func TestSyncWaitsForInflightAsyncNoReorder(t *testing.T) {
	// P1-3: 异步补充 in-flight 时，同步补充必须等待而不是并行发起
	var calls atomic.Int32
	var mu sync.Mutex
	batches := [][]int64{}
	asyncStarted := make(chan struct{})
	asyncProceed := make(chan struct{})

	c := newTestClient(10, func(ctx context.Context, bizTag string) ([]int64, error) {
		n := calls.Add(1)
		if n == 1 {
			// 第一次：返回慢速异步批次 [100..109]
			close(asyncStarted)
			<-asyncProceed
			ids := make([]int64, 10)
			for i := range ids {
				ids[i] = int64(100 + i)
			}
			mu.Lock()
			batches = append(batches, ids)
			mu.Unlock()
			return ids, nil
		}
		// 第二次（不应发生）
		ids := []int64{1, 2, 3}
		mu.Lock()
		batches = append(batches, ids)
		mu.Unlock()
		return ids, nil
	})

	// 先取一个 ID 触发异步补充
	c.cfg.Batch = 10
	tb := c.get("tag")
	// 手动触发 asyncRefill（需要低于水位：1 < 10*0.2=2）
	tb.mu.Lock()
	tb.ids = []int64{50}
	c.asyncRefill(tb, "tag")
	tb.mu.Unlock()

	<-asyncStarted // 等异步发起

	// 现在清空缓冲并调用 Next（同步路径）——应该等待而不是另发请求
	tb.mu.Lock()
	tb.ids = nil
	tb.mu.Unlock()

	// 在异步完成前调用 Next——应等待 in-flight
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(asyncProceed) // 释放异步
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	id, err := c.Next(ctx, "tag")
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}

	mu.Lock()
	totalCalls := len(batches)
	mu.Unlock()
	if totalCalls != 1 {
		t.Fatalf("total fetch calls = %d, want 1 (no reorder)", totalCalls)
	}
	// ID 应该来自异步批次 [100..109]
	if id < 100 || id > 109 {
		t.Fatalf("id = %d, want in [100..109]", id)
	}
}

func TestMergeRejectsNonMonotonicBatch(t *testing.T) {
	tb := &tagBuffer{}
	err := mergeLocked(tb, []int64{5, 3, 7})
	if err == nil {
		t.Fatal("非递增批次应被拒绝")
	}
}

func TestMergeRejectsEmptyBatch(t *testing.T) {
	tb := &tagBuffer{}
	err := mergeLocked(tb, []int64{})
	if err == nil {
		t.Fatal("空批次应被拒绝")
	}
}

func TestMergeRejectsStaleBatch(t *testing.T) {
	tb := &tagBuffer{lastIssued: 100}
	err := mergeLocked(tb, []int64{50, 60})
	if err == nil {
		t.Fatal("过期批次（首 ID <= lastIssued）应被拒绝")
	}
}

func TestMergeAcceptsValidBatch(t *testing.T) {
	tb := &tagBuffer{lastIssued: 10}
	err := mergeLocked(tb, []int64{11, 12, 13})
	if err != nil {
		t.Fatalf("合法批次被拒绝: %v", err)
	}
	if len(tb.ids) != 3 {
		t.Fatalf("buffer len = %d, want 3", len(tb.ids))
	}
}

func TestNextStrictlyIncreasing(t *testing.T) {
	// 端到端：Next 发出的 ID 必须严格递增
	var next int64
	c := newTestClient(10, func(ctx context.Context, bizTag string) ([]int64, error) {
		base := next
		ids := make([]int64, 10)
		for i := range ids {
			ids[i] = base + int64(i) + 1
		}
		next = base + 10
		return ids, nil
	})

	var prev int64
	for i := 0; i < 50; i++ {
		id, err := c.Next(context.Background(), "tag")
		if err != nil {
			t.Fatalf("Next #%d error = %v", i, err)
		}
		if id <= prev {
			t.Fatalf("ID 未递增: %d <= %d (i=%d)", id, prev, i)
		}
		prev = id
	}
}

func TestWaiterRetriesAfterFailedFlight(t *testing.T) {
	// 第一次补充失败，第二次重试成功
	var calls atomic.Int32
	c := newTestClient(5, func(ctx context.Context, bizTag string) ([]int64, error) {
		if calls.Add(1) == 1 {
			return nil, fmt.Errorf("transient error")
		}
		return []int64{1, 2, 3, 4, 5}, nil
	})

	tb := c.get("tag")

	// 第一次补充失败
	err := c.refill(context.Background(), tb, "tag", "sync")
	if err == nil {
		t.Fatal("第一次补充应失败")
	}

	// 第二次补充应成功
	err = c.refill(context.Background(), tb, "tag", "sync")
	if err != nil {
		t.Fatalf("第二次补充 error = %v", err)
	}
	tb.mu.Lock()
	n := len(tb.ids)
	tb.mu.Unlock()
	if n != 5 {
		t.Fatalf("buffer len = %d, want 5", n)
	}
}
