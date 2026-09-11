// Package config 提供共享配置类型与通用加载器。
//
// 职责划分：
//   - 本文件只放各服务共用的配置结构（日志、HTTP 服务）
//   - 加载机制（基础 + APP_ENV 环境覆盖合并）在同包 viper.go
//   - 各服务自己定义根 Config 并组合这些类型，用 LoadInto 加载
package config

import "time"

// ServerConfig HTTP 服务配置
type ServerConfig struct {
	Port           int           `mapstructure:"port"`            // 监听端口
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`    // 读超时，0 表示不限制
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`   // 写超时，0 表示不限制
	TrustedProxies []string      `mapstructure:"trusted_proxies"` // 可信反向代理 IP 或 CIDR
}

// LogConfig 日志配置
type LogConfig struct {
	Level   string `mapstructure:"level"`   // debug | info | warn | error
	Dir     string `mapstructure:"dir"`     // 日志文件存放目录
	Console bool   `mapstructure:"console"` // 是否同时输出到控制台
	Format  string `mapstructure:"format"` // text | json
	Buffer  int    `mapstructure:"buffer"`  // 异步写入缓冲区大小，满则丢弃

	// 以下为文件滚动策略
	MaxSize          int    `mapstructure:"max_size"`          // 单文件上限（MB），写满即切分
	MaxBackups       int    `mapstructure:"max_backups"`       // 保留的历史文件个数，0 表示不限制
	MaxAge           int    `mapstructure:"max_age"`           // 历史文件保留天数，0 表示不限制
	Compress         string `mapstructure:"compress"`          // none | gzip | zstd
	RotationInterval string `mapstructure:"rotation_interval"` // 时间滚动间隔，如 24h；留空或 0 则仅按大小滚动
}
