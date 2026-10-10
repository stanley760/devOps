// gRPC 层发号性能基准：bufconn 进程内回环，覆盖 protobuf 编解码 +
// gRPC 栈开销。与 domain/idgen 的 bench_test.go 对照，可分离出传输成本。
//
// 运行：
//
//	go test ./internal/interfaces/grpc -run '^$' -bench . -benchmem
package grpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	idapp "devops-id/internal/application/idgen"
	iddomain "devops-id/internal/domain/idgen"
	"devops-id/pkg/idgenpb"
)

// quietLogsOnce 保证只在首个基准初始化时屏蔽一次日志。
var quietLogsOnce sync.Once

// quietLogs 屏蔽发号器 info 日志：每轮 b.N 标定都会重建 Manager，
// ready/closed 会刷屏污染基准输出。仅基准路径调用。
func quietLogs() {
	quietLogsOnce.Do(func() {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	})
}

// newBenchClient 启动进程内 IdGen 服务并返回客户端。
// Manager 走本地模式（无 etcd），让基准只测发号与传输栈，不含协调 I/O。
func newBenchClient(b *testing.B) idgenpb.IdGenClient {
	b.Helper()
	quietLogs()
	mgr, err := iddomain.NewManager(iddomain.Config{}, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(mgr.Close)

	lis := bufconn.Listen(1 << 20)
	server := grpcgo.NewServer()
	idgenpb.RegisterIdGenServer(server, NewIdGenServer(idapp.NewService(mgr)))
	go func() { _ = server.Serve(lis) }()
	b.Cleanup(server.Stop)

	conn, err := grpcgo.NewClient(
		"passthrough:///bufnet",
		grpcgo.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpcgo.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = conn.Close() })
	return idgenpb.NewIdGenClient(conn)
}

// BenchmarkGRPCNextSerial 串行 Next RPC：单次调用端到端成本。
func BenchmarkGRPCNextSerial(b *testing.B) {
	client := newBenchClient(b)
	ctx := context.Background()
	req := &idgenpb.NextRequest{BizTag: "bench"}
	// 预热：触发 biz_tag 懒初始化并建立 HTTP/2 流量。
	if _, err := client.Next(ctx, req); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Next(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGRPCNextParallel 并行 Next RPC：传输栈 + 服务端锁竞争的合成吞吐。
func BenchmarkGRPCNextParallel(b *testing.B) {
	client := newBenchClient(b)
	ctx := context.Background()
	req := &idgenpb.NextRequest{BizTag: "bench"}
	if _, err := client.Next(ctx, req); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := client.Next(ctx, req); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

// BenchmarkGRPCBatchNext 批量 RPC：摊掉单次调用成本后接近领域层天花板。
func BenchmarkGRPCBatchNext(b *testing.B) {
	for _, size := range []int{100, 1000} {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			client := newBenchClient(b)
			ctx := context.Background()
			req := &idgenpb.BatchNextRequest{BizTag: "bench", Count: int32(size)}
			if _, err := client.BatchNext(ctx, req); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := client.BatchNext(ctx, req); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.N*size)/b.Elapsed().Seconds(), "ids/s")
		})
	}
}
