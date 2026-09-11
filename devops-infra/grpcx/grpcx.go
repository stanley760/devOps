// Package grpcx 组装 gRPC 服务端与客户端。
//
// 服务端：recovery 拦截器（panic → Internal，堆栈进日志）、accesslog 拦截器、
// 标准健康检查（grpc.health.v1），dev 环境开启 reflection 便于 grpcurl 调试。
// 客户端：拨号封装，内置 Unavailable 指数退避重试（对发号类幂等调用安全）。
package grpcx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// ServerConfig gRPC 服务配置
type ServerConfig struct {
	Listen     string // 监听地址，如 ":9090"
	Reflection bool   // 是否开启 reflection（建议 dev 环境）
}

// Run 启动 gRPC 服务，收到 SIGINT/SIGTERM 后优雅退出。
// register 由各服务调用，用于注册自身的 gRPC 服务实现与健康状况。
func Run(cfg ServerConfig, register func(s *grpc.Server, hc *health.Server)) error {
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			recoveryInterceptor(),
			accessLogInterceptor(),
		),
	)

	hc := health.NewServer()
	healthpb.RegisterHealthServer(s, hc)
	if register != nil {
		register(s, hc)
	}
	if cfg.Reflection {
		reflection.Register(s)
	}

	lis, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", cfg.Listen, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("gRPC 服务启动", "addr", cfg.Listen)
		if err := s.Serve(lis); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("gRPC 服务异常: %w", err)
	case <-ctx.Done():
	}

	slog.Info("收到退出信号，开始优雅关闭")
	stopped := make(chan struct{})
	go func() {
		s.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		s.Stop() // 超时强停，避免退出流程卡死
	}
	slog.Info("gRPC 服务已停止")
	return nil
}

// recoveryInterceptor 把 handler 的 panic 转成 Internal 错误，
// 堆栈进日志，不向客户端泄露细节（对齐 HTTP 侧 Recovery 中间件语义）。
func recoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("gRPC panic", "method", info.FullMethod, "panic", r,
					"stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "服务器内部错误")
			}
		}()
		return handler(ctx, req)
	}
}

// accessLogInterceptor 记录每个 gRPC 请求完成后的访问信息（Debug 级）。
func accessLogInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		startedAt := time.Now()
		resp, err := handler(ctx, req)

		slog.Debug("gRPC 请求",
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"latency", time.Since(startedAt),
		)
		return resp, err
	}
}

// Dial 拨号 gRPC 服务，返回的就绪连接（非阻塞，可重连）。
// 采用内置 Unavailable 指数退避重试策略：发号类调用幂等安全（重试至多烧号，绝不重号）。
func Dial(ctx context.Context, endpoint string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("拨号 gRPC %s 失败: %w", endpoint, err)
	}
	return conn, nil
}

// IsUnavailable 判断错误是否为 Unavailable（连接断开/服务不可达），
// 供调用方决定是否重试。
func IsUnavailable(err error) bool {
	return status.Code(err) == codes.Unavailable
}

// Close 关闭客户端连接。
func Close(conn *grpc.ClientConn) error {
	if conn == nil {
		return nil
	}
	if err := conn.Close(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("关闭 gRPC 连接失败: %w", err)
	}
	return nil
}
