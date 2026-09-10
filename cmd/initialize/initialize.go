// Package initialize 负责服务启动前的统一初始化任务。
//
// 配置加载、日志初始化等公共逻辑统一挂在这里，由 cobra 的
// PersistentPreRunE 触发，因此所有子命令都会自动完成初始化，
// 无需在每个命令里重复一遍。
package initialize

import (
	"github.com/spf13/cobra"

	"devOps/internal/config"
	"devOps/internal/logger"
)

// Options 初始化参数
type Options struct {
	ConfigPath string
}

// NewOptions 返回带默认值的初始化参数
func NewOptions() *Options {
	return &Options{ConfigPath: "config/config.yaml"}
}

// AddFlags 把初始化相关参数注册为持久 flag，所有子命令都能用
func AddFlags(cmd *cobra.Command, opts *Options) {
	cmd.PersistentFlags().StringVarP(
		&opts.ConfigPath, "config", "c", opts.ConfigPath, "配置文件路径",
	)
}

// Setup 执行统一的初始化任务：加载配置 -> 初始化日志。
// 返回配置供后续使用；出错时调用方应终止启动。
func Setup(opts *Options) (*config.Config, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	if err := logger.Init(cfg.Log); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Shutdown 执行统一的收尾任务：排空异步缓冲区并关闭日志文件。
func Shutdown() error {
	return logger.Close()
}
