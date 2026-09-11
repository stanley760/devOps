// Package cmd 定义 devOps 的命令行入口。
//
// 配置管理：路径由 -f/--config-file 绑定，加载与读取封装在 internal/config，
// cobra.OnInitialize 中完成加载，PersistentPreRunE 中完成组件初始化。
package cmd

import (
	"log/slog"
	"github.com/spf13/cobra"
	"devOps/cmd/initialize"
	"devOps/cmd/server"
	"devOps/internal/config"
)

var (
	confFile string
)

var rootCmd = &cobra.Command{
	Use:   "devops",
	Short: "devOps 运维服务",

	// 所有子命令执行前都会先跑这里：初始化日志
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := initialize.Setup()
		if err != nil {
			return err
		}
		// 至此日志已就绪，可以放心使用 slog
		slog.Debug("初始化完成",
			"config", config.Files(),
			"env", config.Env(),
			"logLevel", cfg.Log.Level,
		)
		return nil
	},

	// 所有子命令执行后统一收尾，保证日志不丢
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		return initialize.Shutdown()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// confFile 绑定到 -f/--config-file，与 ecmdb 保持一致
	rootCmd.PersistentFlags().StringVarP(
		&confFile, "config-file", "f", "config/config.yaml", "the service config from file",
	)
}

// Execute 执行根命令。version 用于 --version 输出。
func Execute(version string) {
	rootCmd.Version = version

	// cobra.OnInitialize 会在每个命令的 Run 之前执行，此处完成配置加载
	cobra.OnInitialize(func() {
		if err := config.Init(confFile); err != nil {
			panic(err)
		}
	})
	rootCmd.AddCommand(server.Cmd)
	if err := rootCmd.Execute(); err != nil {
		cobra.CheckErr(err)
	}
}
