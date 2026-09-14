// Package grpc 组装 gRPC 服务端与客户端。
//
// 服务端：recovery 拦截器（panic → Internal，堆栈进日志）、accesslog 拦截器、
// 标准健康检查（grpc.health.v1），dev 环境开启 reflection 便于 grpcurl 调试。
// 客户端：拨号封装，内置 Unavailable 指数退避重试（对发号类幂等调用安全）。
package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"devops-infra/grpc/interceptor"
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
			interceptor.Recovery(),
			interceptor.AccessLog(),
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
