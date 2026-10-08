// idbench 直连 devops-id gRPC 的发号压测工具。
//
// 用途：绕过网关/业务层，直接测发号服务在高并发下的吞吐与延迟，
// 并在压测期间通过 Grafana 的 gRPC 服务端面板（rpc_server_*）观察服务端视角。
//
// 使用示例（在仓库根目录运行）：
//
//	# 4 个 biz_tag、50 并发、batch=100（模拟 idclient 预取形态），跑 30 秒
//	go run ./devops-id/cmd/idbench -addr 127.0.0.1:9990 -method batch -batch 100 -tags b1,b2,b3,b4 -c 50 -d 30s
//
//	# 单 tag 单发（测单 worker 串行 + gRPC 栈开销上限）
//	go run ./devops-id/cmd/idbench -addr 127.0.0.1:9990 -method next -tags user -c 100 -d 15s
//
// 语义要点（决定怎么读结果）：
//   - 同一 biz_tag 在服务端固定映射到同一个 snowflake worker（tagGenerator 互斥锁），
//     单 tag 压测测的是"单 worker 串行 + gRPC 栈"的吞吐上限；
//   - 多个 tag 之间相互独立，-tags 填多个 tag 才能测多 worker 水平扩展；
//   - method=batch + batch=N 等价于 idclient 预取形态：1 次 RPC 摊薄 N 个号，
//     N 越大单号成本越低，但缓冲弹性窗口也越大（消费方视角见 idgen_buffer_capacity）。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"devops-id/pkg/idgenpb"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9990", "devops-id gRPC 地址")
	method := flag.String("method", "batch", "发号方法：next | batch")
	batch := flag.Int("batch", 1, "BatchNext 单次取号数量（method=batch 时生效，1..100000）")
	tags := flag.String("tags", "bench", "biz_tag 列表，逗号分隔；多个 tag 表示压测多个 snowflake worker 并行")
	conc := flag.Int("c", 50, "并发 goroutine 数")
	dur := flag.Duration("d", 10*time.Second, "压测时长（0 表示只按 -n 总请求数跑）")
	total := flag.Int64("n", 0, "总请求数上限（0 表示只按 -d 时长跑）")
	warmup := flag.Duration("warmup", 10*time.Second, "预热时长上限：正式测量前逐 tag 取号，触发 etcd worker 槽位获取与连接建立")
	callTimeout := flag.Duration("timeout", 5*time.Second, "单次调用超时")
	flag.Parse()

	tagList := strings.Split(*tags, ",")
	for i := range tagList {
		tagList[i] = strings.TrimSpace(tagList[i])
	}
	if *method != "next" && *method != "batch" {
		fmt.Fprintf(os.Stderr, "idbench: -method 仅支持 next | batch\n")
		os.Exit(2)
	}
	if *method == "batch" && (*batch < 1 || *batch > 100000) {
		fmt.Fprintf(os.Stderr, "idbench: -batch 须在 1..100000\n")
		os.Exit(2)
	}
	if *dur <= 0 && *total <= 0 {
		fmt.Fprintf(os.Stderr, "idbench: -d 与 -n 至少给一个\n")
		os.Exit(2)
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "idbench: 建立客户端失败: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()
	stub := idgenpb.NewIdGenClient(conn)

	// 预热：逐 tag 成功取号一次（触发服务端 etcd 槽位获取 + gRPC 连接建立），
	// 正式测量不带首次慢调用；超时未就绪则带病开测并提示。
	warmupCtx, warmupCancel := context.WithTimeout(context.Background(), *warmup)
	notReady := warmupAll(warmupCtx, stub, *method, *batch, tagList)
	warmupCancel()
	for _, t := range notReady {
		fmt.Printf("警告: biz_tag=%s 预热期内未成功取号（etcd 槽位获取慢或服务不可用），结果将包含慢启动\n", t)
	}

	if len(tagList) == 1 {
		fmt.Println("提示: 单 biz_tag 在服务端串行于同一 snowflake worker，本次测的是单 worker + gRPC 栈上限；测水平扩展请用 -tags 填多个 tag")
	}

	var (
		sent, okCount, failCount atomic.Int64
		errMu                    sync.Mutex
		errByCode                = map[codes.Code]int64{}
		latMu                    sync.Mutex
		latencies                []time.Duration
	)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	startAt := time.Now()
	deadline := startAt.Add(*dur)

	// 停止条件：时长到 / 请求数到
	stopped := func() bool {
		if *total > 0 && sent.Load() >= *total {
			return true
		}
		return *dur > 0 && time.Now().After(deadline)
	}

	// 每秒进度
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		last, lastAt := int64(0), startAt
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				cur := sent.Load()
				fmt.Printf("  %6.1fs  总请求 %8d  成功 %8d  失败 %6d  瞬时 %.0f/s\n",
					time.Since(startAt).Seconds(), cur, okCount.Load(), failCount.Load(),
					float64(cur-last)/now.Sub(lastAt).Seconds())
				last, lastAt = cur, now
			case <-ctx.Done():
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < *conc; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tag := tagList[i%len(tagList)]
			local := make([]time.Duration, 0, 1024)
			defer func() {
				latMu.Lock()
				latencies = append(latencies, local...)
				latMu.Unlock()
			}()
			for !stopped() {
				cctx, ccancel := context.WithTimeout(context.Background(), *callTimeout)
				t0 := time.Now()
				var err error
				if *method == "batch" {
					_, err = stub.BatchNext(cctx, &idgenpb.BatchNextRequest{BizTag: tag, Count: int32(*batch)})
				} else {
					_, err = stub.Next(cctx, &idgenpb.NextRequest{BizTag: tag})
				}
				local = append(local, time.Since(t0))
				ccancel()
				sent.Add(1)
				if err != nil {
					failCount.Add(1)
					errMu.Lock()
					errByCode[status.Code(err)]++
					errMu.Unlock()
					continue
				}
				okCount.Add(1)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(startAt)
	stop()
	<-progressDone

	// 汇总
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	sentN := sent.Load()
	fmt.Println()
	fmt.Printf("======== 压测结果 ========\n")
	fmt.Printf("目标 %s  方法 %s  batch=%d  tags=%s  并发 %d  时长 %.1fs\n",
		*addr, *method, *batch, *tags, *conc, elapsed.Seconds())
	fmt.Printf("总请求 %d  成功 %d  失败 %d (%.2f%%)\n",
		sentN, okCount.Load(), failCount.Load(), pct(failCount.Load(), sentN))
	if elapsed.Seconds() > 0 {
		fmt.Printf("QPS %.1f（每秒 RPC 数）；method=batch 时单号吞吐 ≈ %.1f/s\n",
			float64(sentN)/elapsed.Seconds(), float64(sentN)*float64(*batch)/elapsed.Seconds())
	}
	if n := len(latencies); n > 0 {
		var sum time.Duration
		for _, d := range latencies {
			sum += d
		}
		fmt.Printf("延迟  avg %s  P50 %s  P90 %s  P95 %s  P99 %s  P99.9 %s  max %s\n",
			round(sum/time.Duration(n)),
			round(pctile(latencies, 0.50)), round(pctile(latencies, 0.90)), round(pctile(latencies, 0.95)),
			round(pctile(latencies, 0.99)), round(pctile(latencies, 0.999)), round(latencies[n-1]))
	}
	if len(errByCode) > 0 {
		fmt.Printf("失败分布: ")
		for c, n := range errByCode {
			fmt.Printf("%s=%d(%s) ", c.String(), n, codeName(c))
		}
		fmt.Println()
	}
}

// warmupAll 逐 tag 预热直到各自成功一次或超时，返回未就绪 tag。
func warmupAll(ctx context.Context, stub idgenpb.IdGenClient, method string, batch int, tags []string) []string {
	var notReady []string
	for _, tag := range tags {
		ok := false
		for !ok {
			cctx, ccancel := context.WithTimeout(ctx, 3*time.Second)
			var err error
			if method == "batch" {
				_, err = stub.BatchNext(cctx, &idgenpb.BatchNextRequest{BizTag: tag, Count: int32(batch)})
			} else {
				_, err = stub.Next(cctx, &idgenpb.NextRequest{BizTag: tag})
			}
			ccancel()
			if err == nil {
				ok = true
				break
			}
			if ctx.Err() != nil {
				break
			}
		}
		if !ok {
			notReady = append(notReady, tag)
		}
	}
	return notReady
}

func pctile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

func round(d time.Duration) time.Duration {
	switch {
	case d > time.Millisecond:
		return d.Round(100 * time.Microsecond)
	case d > time.Microsecond:
		return d.Round(100 * time.Nanosecond)
	default:
		return d
	}
}

func pct(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// codeName 常见 gRPC 错误码中文名，便于读结果。
func codeName(c codes.Code) string {
	switch c {
	case codes.Unavailable:
		return "服务不可达/lease失效/时钟回拨"
	case codes.InvalidArgument:
		return "参数非法"
	case codes.DeadlineExceeded:
		return "调用超时"
	case codes.Internal:
		return "服务内部错误"
	default:
		return ""
	}
}
