// Package cmd 定义 devOps 的命令行入口。
//
// 初始化与收尾分别挂在 cobra 的 PersistentPreRunE / PersistentPostRunE 上，
// 任何子命令执行前后都会自动完成，新增子命令时无需关心这些公共逻辑。
package cmd

import (
	"log/slog"

	"github.com/spf13/cobra"

	"devOps/cmd/initialize"
)

var (
	initOpts   = initialize.NewOptions()
	appVersion string
)

var rootCmd = &cobra.Command{
	Use:   "devops",
	Short: "devOps 运维服务",

	// 所有子命令执行前都会先跑这里：加载配置、初始化日志
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := initialize.Setup(initOpts)
		if err != nil {
			return err
		}
		// 至此日志已就绪，可以放心使用 slog
		slog.Debug("初始化完成", "config", initOpts.ConfigPath, "logLevel", cfg.Log.Level)
		return nil
	},

	// 所有子命令执行后统一收尾，保证日志不丢
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		return initialize.Shutdown()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		// 演示不同级别的日志输出。配置 log.level 后可观察哪些会被过滤。
		slog.Debug("调试信息：细粒度诊断，通常只在排查问题时开启", "module", "boot")
		slog.Info("服务已就绪", "version", appVersion)
		slog.Warn("配置告警：可继续工作但建议检查", "hint", "示例提示")
		slog.Error("错误信息：发生了需要关注的问题", "err", "示例错误")
		return nil
	},
}

func init() {
	initialize.AddFlags(rootCmd, initOpts)
}

// Execute 执行根命令。version 用于 --version 输出。
func Execute(version string) error {
	appVersion = version
	rootCmd.Version = version
	return rootCmd.Execute()
}
