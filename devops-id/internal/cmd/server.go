// server 子命令：装配 db → segment → gRPC 并启动。
package cmd

import (
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	infracfg "devops-infra/config"
	"devops-infra/grpcx"

	"devops-id/internal/config"
	"devops-id/internal/db"
	"devops-id/internal/segment"
	"devops-id/internal/server"
	"devops-id/pkg/idgenpb"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动 gRPC 发号服务",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		// 号段表依赖 MySQL
		if err := db.Init(cfg.Mysql); err != nil {
			return err
		}

		mgr := segment.New(db.Get(), segment.Config{
			Step:     cfg.IDGen.Step,
			LowWater: cfg.IDGen.LowWater,
		})

		reflection := cfg.Grpc.Reflection
		if infracfg.Env() == "dev" {
			reflection = true // dev 环境强制开启，便于 grpcurl 调试
		}

		return grpcx.Run(grpcx.ServerConfig{
			Listen:     cfg.Grpc.Listen,
			Reflection: reflection,
		}, func(s *grpc.Server, hc *health.Server) {
			idgenpb.RegisterIdGenServer(s, server.NewIdGenServer(mgr))
		hc.SetServingStatus("idgen.IdGen", healthpb.HealthCheckResponse_SERVING)
		})
	},
}
