// Package config 定义应用配置结构。
//
// 配置的加载由同包的 viper.go 完成（Init/Load），这里只放结构体定义。
// 配置完全由 config.yaml 提供，不在代码里设置默认值。
package config

import "time"

// Config 应用根配置，对应 yaml 顶层结构
type Config struct {
	Log    LogConfig    `mapstructure:"log"`
	Server ServerConfig `mapstructure:"server"`
}

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
	Format  string `mapstructure:"format"`  // text | json
	Buffer  int    `mapstructure:"buffer"`  // 异步写入缓冲区大小，满则丢弃

	// 以下为文件滚动策略
	MaxSize          int    `mapstructure:"max_size"`          // 单文件上限（MB），写满即切分
	MaxBackups       int    `mapstructure:"max_backups"`       // 保留的历史文件个数，0 表示不限制
	MaxAge           int    `mapstructure:"max_age"`           // 历史文件保留天数，0 表示不限制
	Compress         string `mapstructure:"compress"`          // none | gzip | zstd
	RotationInterval string `mapstructure:"rotation_interval"` // 时间滚动间隔，如 24h；留空或 0 则仅按大小滚动
}
