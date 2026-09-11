package server

import (
	"github.com/spf13/cobra"

	"devOps/internal/config"
	"devOps/internal/router"
)

// Cmd 启动 HTTP 服务。
// 配置加载与日志初始化已由 root 的 PersistentPreRunE 完成，
// 此处仅需再次读取配置（viper 已解析，代价可忽略）拿到 server 段。
var Cmd = &cobra.Command{
	Use:   "server",
	Short: "启动 HTTP 服务",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return router.Run(cfg.Server)
	},
}

