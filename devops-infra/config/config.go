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
	CORS           CORSConfig    `mapstructure:"cors"`            // 跨域配置（allow_origins 为空则不启用）
}

// CORSConfig 跨域配置（httpx 统一装配，见 middleware.CORS）
type CORSConfig struct {
	AllowOrigins     []string      `mapstructure:"allow_origins"`     // 允许的源，精确匹配；["*"] 通配
	AllowMethods     []string      `mapstructure:"allow_methods"`     // 空：GET/POST/PUT/DELETE/OPTIONS
	AllowHeaders     []string      `mapstructure:"allow_headers"`     // 空：Origin/Content-Type/Authorization/X-Requested-With
	AllowCredentials bool          `mapstructure:"allow_credentials"` // 允许携带 Cookie（为 true 时不回显 *）
	MaxAge           time.Duration `mapstructure:"max_age"`           // 预检缓存时长，如 12h
}
