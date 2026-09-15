// server 子命令：按 idgen.mode 装配发号器（segment → MySQL 双 buffer；
// snowflake → etcd 槽位抢占）并启动 gRPC 服务。
package cmd

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	infracfg "devops-infra/config"
	grpcinfra "devops-infra/grpc"
	"devops-infra/nacos"

	"devops-id/internal/config"
	"devops-id/internal/segment"
	"devops-id/internal/server"
	"devops-id/internal/snowflake"
	"devops-id/pkg/idgenpb"
	client "devops-infra/database/mysql"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动 gRPC 发号服务（segment | snowflake 双模式）",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		// 发号器装配：segment 依赖 MySQL；snowflake 依赖 etcd（抢槽位 + keepalive）。
		// 优雅退出：grpcinfra.Run 收到信号返回后，defer 链 revoke lease 释放槽位（方案 6.6 Shutdown）。
		var gen server.IdGenerator
		switch cfg.IDGen.Mode {
		case "", "segment":
			if err := client.Init(cfg.Mysql); err != nil {
				return err
			}
			gen = segment.New(client.Get(), segment.Config{
				Step:     cfg.IDGen.Step,
				LowWater: cfg.IDGen.LowWater,
			})

		case "snowflake":
			if len(cfg.Etcd.Endpoints) == 0 {
				return fmt.Errorf("idgen.mode=snowflake 需要配置 etcd.endpoints")
			}
			if cfg.Etcd.DialTimeout <= 0 {
				cfg.Etcd.DialTimeout = 5 * time.Second
			}
			cli, err := clientv3.New(clientv3.Config{
				Endpoints:   cfg.Etcd.Endpoints,
				DialTimeout: cfg.Etcd.DialTimeout,
			})
			if err != nil {
				return fmt.Errorf("连接 etcd 失败: %w", err)
			}
			defer cli.Close()

			// 抢槽位含 1024 槽依序扫描，预算 30s 超时防启动卡死
			initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			sf, err := snowflake.New(initCtx, cli, snowflake.Config{
				LeaseTTL:    cfg.Etcd.LeaseTTL,
				WorkerCache: cfg.Etcd.WorkerCache,
			})
			if err != nil {
				return err
			}
			defer sf.Close() // 优雅退出：持久化水位 + revoke lease 立即释放槽位
			gen = sf

		default:
			return fmt.Errorf("未知 idgen.mode %q（支持 segment | snowflake）", cfg.IDGen.Mode)
		}

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

		return grpcinfra.Run(grpcinfra.ServerConfig{
			Listen:     cfg.Grpc.Listen,
			Reflection: reflection,
		}, func(s *grpc.Server, hc *health.Server) {
			idgenpb.RegisterIdGenServer(s, server.NewIdGenServer(gen))
			hc.SetServingStatus("idgen.IdGen", healthpb.HealthCheckResponse_SERVING)
		})
	},
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
