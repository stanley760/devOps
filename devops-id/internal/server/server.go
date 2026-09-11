// Package server 提供 devops-id 的 server 子命令：启动 gRPC 发号服务。
package server

import (
	"github.com/spf13/cobra"

	infracfg "devops-infra/config"
	"devops-infra/grpcx"

	"devops-id/internal/config"
)

// Cmd 启动 gRPC 服务。
// 配置加载与日志初始化已由 root 的 PersistentPreRunE 完成。
var Cmd = &cobra.Command{
	Use:   "server",
	Short: "启动 gRPC 发号服务",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		reflection := cfg.Grpc.Reflection
		if infracfg.Env() == "dev" {
			reflection = true // dev 环境强制开启，便于 grpcurl 调试
		}

		return grpcx.Run(grpcx.ServerConfig{
			Listen:     cfg.Grpc.Listen,
			Reflection: reflection,
		}, nil) // IdGen 服务注册随 P2 交付
	},
}
