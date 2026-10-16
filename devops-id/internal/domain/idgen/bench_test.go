// 发号性能基准（Go testing.B）。三层背压来源各有对应基准：
//
//  1. g.mu 串行化 —— BenchmarkNextParallel / BenchmarkBackpressure 的锁竞争；
//  2. 4096/ms 序号天花板 —— 序号耗尽时 nextLocked 持锁等下一毫秒，
//     BenchmarkNextSerial / BenchmarkBackpressure 的吞吐平台期即此；
//  3. batchNext 长持锁 —— BenchmarkMixedBatchVsNext 观察批量对单发号的挤压。
//
// 运行：
//
//	go test ./internal/domain/idgen -run '^$' -bench . -benchmem
//	go test ./internal/domain/idgen -run '^$' -bench Backpressure -benchtime 2s
//	go test ./internal/domain/idgen -run '^$' -bench . -cpu 1,4 -count 3
package idgen

import (
	"context"
	"fmt"
	"math/bits"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 延迟直方图：0..204.8µs 按 20ns 分桶（热路径几十纳秒的延迟可区分），
// 超出部分按对数分桶（覆盖背压排队的毫秒级尾延迟，误差可接受）。
// 全程无堆分配，b.N 到千万级也不会撑爆内存。
const (
	histTickNs         = 20    // 精确桶宽（20ns）
	histPreciseBuckets = 10240 // 精确范围 10240*20ns = 204.8µs
	histLogBuckets     = 32    // ≥精确范围后的对数桶数
)

type latencyHist struct {
	fine  [histPreciseBuckets]atomic.Uint64
	logB  [histLogBuckets]atomic.Uint64
	count atomic.Uint64
	maxNs atomic.Int64
}

func (h *latencyHist) record(d time.Duration) {
	ns := d.Nanoseconds()
	if ns < 0 {
		ns = 0
	}
	h.count.Add(1)
	for {
		old := h.maxNs.Load()
		if ns <= old || h.maxNs.CompareAndSwap(old, ns) {
			break
		}
	}
	ticks := ns / histTickNs
	if ticks < histPreciseBuckets {
		h.fine[ticks].Add(1)
		return
	}
	idx := bits.Len64(uint64(ticks) / histPreciseBuckets)
	if idx >= histLogBuckets {
		idx = histLogBuckets - 1
	}
	h.logB[idx].Add(1)
}

// percentile 返回第 p 分位延迟（0<p<=1）。精确桶取桶中点，对数桶取上界近似。
func (h *latencyHist) percentile(p float64) time.Duration {
	total := h.count.Load()
	if total == 0 {
		return 0
	}
	want := uint64(float64(total) * p)
	if want == 0 {
		want = 1
	}
	var cum uint64
	for i := 0; i < histPreciseBuckets; i++ {
		cum += h.fine[i].Load()
		if cum >= want {
			return time.Duration(i*histTickNs + histTickNs/2)
		}
	}
	for i := 0; i < histLogBuckets; i++ {
		cum += h.logB[i].Load()
		if cum >= want {
			return time.Duration(histPreciseBuckets<<i) * histTickNs
		}
	}
	return time.Duration(histPreciseBuckets<<(histLogBuckets-1)) * histTickNs
}

// runFixedWorkers 以固定并发闭环压测：workers 个协程瓜分 b.N 次 op，
// 每次调用前后打点记入直方图。固定并发（而非 RunParallel 的 GOMAXPROCS）
// 才能扫出完整的“并发数→吞吐/延迟”背压曲线。
func runFixedWorkers(b *testing.B, workers int, op func() error) *latencyHist {
	b.Helper()
	hist := &latencyHist{}
	var (
		errOnce sync.Once
		errBox  error
		failed  atomic.Bool
		wg      sync.WaitGroup
		start   = make(chan struct{})
	)
	fail := func(err error) {
		errOnce.Do(func() { errBox = err })
		failed.Store(true)
	}

	base := b.N / workers
	rem := b.N % workers
	for w := 0; w < workers; w++ {
		n := base
		if w < rem {
			n++
		}
		if n == 0 {
			continue
		}
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			for i := 0; i < n; i++ {
				if failed.Load() {
					return
				}
				startedAt := time.Now()
				if err := op(); err != nil {
					fail(err)
					return
				}
				hist.record(time.Since(startedAt))
			}
		}(n)
	}

	b.ResetTimer()
	close(start)
	wg.Wait()
	b.StopTimer()
	if errBox != nil {
		b.Fatalf("发号失败: %v", errBox)
	}
	return hist
}

// reportHist 输出背压观测指标：吞吐（ids/s）与延迟分位。
// 尾部分位看 p999/max：序号耗尽约每 4096 个号发生一次（≈0.02%），
// p99 看不到它，p999 才是“撞天花板”信号。
// 调用前需已 StopTimer（runFixedWorkers 已保证）。
func reportHist(b *testing.B, h *latencyHist) {
	b.Helper()
	if n := h.count.Load(); n > 0 {
		b.ReportMetric(float64(n)/b.Elapsed().Seconds(), "ids/s")
		b.ReportMetric(float64(h.percentile(0.50).Nanoseconds())/1000, "p50-us")
		b.ReportMetric(float64(h.percentile(0.99).Nanoseconds())/1000, "p99-us")
		b.ReportMetric(float64(h.percentile(0.999).Nanoseconds())/1000, "p999-us")
		b.ReportMetric(float64(h.maxNs.Load())/1000, "max-us")
	}
}

// BenchmarkNextSerial 串行发号：真实时钟，吞吐被 4096/ms 序号天花板
// 锁死在约 4M ids/s（等下一毫秒的等待时间摊进 ns/op）。
func BenchmarkNextSerial(b *testing.B) {
	gen := newTestGenerator(1)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := gen.next(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNextRawCPU 纯热路径成本：nowFn 每次调用推进 1ms，序号永不耗尽，
// 测出互斥锁 + 时间戳比较 + 位运算的裸 CPU 成本（不含任何等待）。
func BenchmarkNextRawCPU(b *testing.B) {
	gen := newTestGenerator(1)
	var fakeMs atomic.Int64
	fakeMs.Store(time.Now().UnixMilli())
	gen.nowFn = func() int64 { return fakeMs.Add(1) }
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := gen.next(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNextParallel 锁竞争 + 序号天花板下的并行吞吐（GOMAXPROCS 个协程）。
// 吞吐不再随 -cpu 线性增长时即到达天花板。
func BenchmarkNextParallel(b *testing.B) {
	gen := newTestGenerator(1)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := gen.next(ctx); err != nil {
				// FailNow 不能从非基准协程调用，用 Error 记录后退出。
				b.Error(err)
				return
			}
		}
	})
}

// BenchmarkBackpressure 背压主基准：并发数扫掠，观察吞吐平台期与延迟爬升。
//
//   - ids/s 在某个并发后不再增长 = 撞上序号天花板 / 锁瓶颈；
//   - p99-us 随并发继续爬升 = 请求在 g.mu 上排队（背压传导为延迟）。
func BenchmarkBackpressure(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8, 16, 32, 64, 128} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			gen := newTestGenerator(1)
			ctx := context.Background()
			hist := runFixedWorkers(b, workers, func() error {
				_, err := gen.next(ctx)
				return err
			})
			reportHist(b, hist)
		})
	}
}

// BenchmarkBatchNext 批量摊销：一次锁内取 count 个号。
// ns/op 是每批成本，ids/s 是摊到单号的吞吐（count 越大越接近天花板）。
func BenchmarkBatchNext(b *testing.B) {
	for _, size := range []int{1, 10, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			gen := newTestGenerator(1)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := gen.batchNext(ctx, size); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.N*size)/b.Elapsed().Seconds(), "ids/s")
		})
	}
}

// BenchmarkMixedBatchVsNext 批量背压传导：一个协程持续取 10000 的号段
// （batchNext 长持锁，每 1024 个才短暂放锁），workers 个协程高频 Next。
// 对照 BenchmarkBackpressure 同并发的 p99，差值即批量挤压单发号的代价。
func BenchmarkMixedBatchVsNext(b *testing.B) {
	const batchSize = 10000
	for _, workers := range []int{4, 16} {
		b.Run(fmt.Sprintf("next-workers-%d", workers), func(b *testing.B) {
			gen := newTestGenerator(1)
			ctx := context.Background()
			stop := make(chan struct{})
			var batchWG sync.WaitGroup
			batchWG.Add(1)
			go func() {
				defer batchWG.Done()
				for {
					select {
					case <-stop:
						return
					default:
						if _, err := gen.batchNext(ctx, batchSize); err != nil {
							return
						}
					}
				}
			}()

			hist := runFixedWorkers(b, workers, func() error {
				_, err := gen.next(ctx)
				return err
			})
			close(stop)
			batchWG.Wait()
			reportHist(b, hist)
		})
	}
}
