// Package cmd 定义 devops-system 的命令行入口。
//
// 配置管理：路径由 -f/--config-file 绑定，加载与读取封装在 devops-infra/config，
// cobra.OnInitialize 中完成加载，PersistentPreRunE 中完成组件初始化。
package cmd

import (
	"log/slog"

	"github.com/spf13/cobra"

	infracfg "devops-infra/config"
	"devops-infra/httpx"
	"devops-infra/logger"

	"devops-system/internal/config"
	"devops-system/internal/router"
)

var confFile string

var rootCmd = &cobra.Command{
	Use:   "devops-system",
	Short: "devOps 用户权限系统",

	// 所有子命令执行前都会先跑这里：初始化日志
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}
		if err := logger.Init(cfg.Log); err != nil {
			return err
		}

		slog.Debug("初始化完成",
			"service", "devops-system",
			"config", infracfg.Files(),
			"env", infracfg.Env(),
			"logLevel", cfg.Log.Level,
		)
		return nil
	},

	// 所有子命令执行后统一收尾，保证日志不丢
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		return logger.Close()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&confFile, "config-file", "f", "config/config.yaml", "the service config from file",
	)
}

// Execute 执行根命令。version 用于 --version 输出。
func Execute(version string) {
	rootCmd.Version = version

	// cobra.OnInitialize 会在每个命令的 Run 之前执行，此处完成配置加载
	cobra.OnInitialize(func() {
		if err := infracfg.Init(confFile); err != nil {
			panic(err)
		}
	})
	rootCmd.AddCommand(serverCmd)
	if err := rootCmd.Execute(); err != nil {
		cobra.CheckErr(err)
	}
}

// serverCmd 启动 HTTP 服务。
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动 HTTP 服务",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}
		return httpx.Run(cfg.Server, router.Register())
	},
}
