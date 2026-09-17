// server 子命令：装配 snowflake 发号器并启动 gRPC 服务。
package cmd

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	infracfg "devops-infra/config"
	etcdinfra "devops-infra/etcd"
	grpcinfra "devops-infra/grpc"
	"devops-infra/nacos"

	"devops-id/internal/config"
	"devops-id/internal/server"
	"devops-id/internal/snowflake"
	"devops-id/pkg/idgenpb"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动 gRPC snowflake 发号服务",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		// 发号器装配：snowflake 依赖 etcd；worker slot 在首次使用 biz_tag 时懒加载。
		cli, err := etcdinfra.New(cfg.Etcd)
		if err != nil {
			return err
		}
		defer cli.Close()

		sf, err := snowflake.New(context.Background(), cli, snowflake.Config{
			LeafName:    cfg.IDGen.LeafName,
			LeaseTTL:    cfg.Etcd.LeaseTTL,
			WorkerCache: cfg.IDGen.WorkerCache,
		})
		if err != nil {
			return err
		}
		defer sf.Close() // 优雅退出：revoke 所有 per-tag lease，立即释放槽位

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
			idgenpb.RegisterIdGenServer(s, server.NewIdGenServer(sf))
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
