// server 子命令：装配依赖图（wire）并启动 gRPC 服务与 metrics。
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	// 挂载 /debug/pprof/* 到 DefaultServeMux（见 startMetricsServer）
	"github.com/spf13/cobra"
	"github.com/stanley760/devOps/devops-id/internal/app"
	"github.com/stanley760/devOps/devops-id/internal/config"
	infracfg "github.com/stanley760/devOps/devops-infra/config"
	grpcinfra "github.com/stanley760/devOps/devops-infra/grpc"
	"github.com/stanley760/devOps/devops-infra/nacos"
	"github.com/stanley760/devOps/devops-infra/observability"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	_ "net/http/pprof"
	"strconv"
	"time"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动 gRPC snowflake 发号服务",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		// 依赖图装配（wire）：etcd client → per-tag 基础设施工厂 →
		// snowflake Manager → 应用服务 → gRPC handler；worker slot 在
		// 首次使用 biz_tag 时懒加载。
		register, cleanupGraph, err := app.InitializeGRPC(cfg)
		if err != nil {
			return err
		}
		defer cleanupGraph() // 优雅退出：revoke 所有 per-tag lease，立即释放槽位，再关 etcd

		reflection := cfg.Grpc.Reflection
		if infracfg.Env() == "dev" {
			reflection = true // dev 环境强制开启，便于 grpcurl 调试
		}

		serviceName := cfg.Nacos.Registry.ServiceName
		if serviceName == "" {
			return fmt.Errorf("nacos.registry.service_name 未配置")
		}
		port, err := listenPort(cfg.Grpc.Listen)
		if err != nil {
			return err
		}
		if err := nacos.Register(serviceName, port, cfg.Nacos.Registry.Meta); err != nil {
			return err
		}
		defer func() {
			_ = nacos.Deregister(serviceName, port)
		}()

		grpcErr := make(chan error, 1)
		go func() {
			grpcErr <- grpcinfra.Run(grpcinfra.ServerConfig{
				Listen:       cfg.Grpc.Listen,
				Reflection:   reflection,
				StatsHandler: otelgrpc.NewServerHandler(),
			}, register)
		}()

		metricsServer, metricsErr, err := startMetricsServer(obsRuntime, cfg.Observability.MetricsListen)
		if err != nil {
			return err
		}
		defer stopMetricsServer(metricsServer)

		select {
		case err := <-metricsErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return <-grpcErr
		case err := <-grpcErr:
			return err
		}
	},
}

func startMetricsServer(obs *observability.Runtime, listen string) (*http.Server, <-chan error, error) {
	if listen == "" || obs == nil || obs.MetricsHandler == nil {
		return nil, make(chan error, 1), nil
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, nil, fmt.Errorf("监听 metrics %s 失败: %w", listen, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", obs.MetricsHandler)
	// 诊断出口：goroutine/block 等 profile 用于排查 handler 卡死类故障
	// （2026-10-08 BatchNext 疑似卡死无现场可查）。仅 metrics_listen 配置时生效。
	mux.Handle("/debug/pprof/", http.DefaultServeMux)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	return server, errCh, nil
}

func stopMetricsServer(server *http.Server) {
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func listenPort(listen string) (int, error) {
	_, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, fmt.Errorf("解析 gRPC 监听地址 %q 失败: %w", listen, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("gRPC 监听地址 %q 的端口无效", listen)
	}
	return port, nil
}
