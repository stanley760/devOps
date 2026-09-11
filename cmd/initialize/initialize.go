// Package initialize 负责服务启动前的统一初始化任务。
//
// 配置来源为 internal/config（内部封装 viper），
// 这里只负责读取配置并初始化对应组件。
// 初始化挂在 cobra 的 PersistentPreRunE 上，所有子命令都会自动完成。
package initialize

import (
	"devOps/internal/config"
	"devOps/internal/logger"
)

// Setup 执行统一的初始化任务：读取配置 -> 初始化日志。
// 返回配置供后续使用。
func Setup() (config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, err
	}

	if err := logger.Init(cfg.Log); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// Shutdown 执行统一的收尾任务：排空异步缓冲区并关闭日志文件。
func Shutdown() error {
	return logger.Close()
}
