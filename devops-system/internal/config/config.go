// Package config 定义 devops-system 服务的配置结构。
//
// 加载机制复用 devops-infra/config（基础 + APP_ENV 环境覆盖合并），
// 本包只定义自己的根 Config 与专有配置段。
package config

import (
	"time"

	"devops-infra/config"
)

// Config devops-system 服务根配置，对应 config/config.yaml 顶层结构
type Config struct {
	Server   config.ServerConfig `mapstructure:"server"`
	Log      config.LogConfig    `mapstructure:"log"`
	Mysql    config.MysqlConfig  `mapstructure:"mysql"`
	Redis    config.RedisConfig  `mapstructure:"redis"`
	JWT      JWTConfig           `mapstructure:"jwt"`
	Tenant   TenantConfig        `mapstructure:"tenant"`
	IDClient IDClientConfig      `mapstructure:"idclient"`
	Nacos    config.NacosConfig  `mapstructure:"nacos"`
}

// JWTConfig JWT 双 token 配置
type JWTConfig struct {
	SigningKey    string        `mapstructure:"signing_key"`    // HS256 签名密钥，至少 32 位随机串
	Issuer        string        `mapstructure:"issuer"`         // 签发者
	AccessExpire  time.Duration `mapstructure:"access_expire"`  // access token 有效期
	RefreshExpire time.Duration `mapstructure:"refresh_expire"` // refresh token（会话）有效期
}

// TenantConfig 多租户配置
type TenantConfig struct {
	Mode           string `mapstructure:"mode"`            // shared 共享Schema（默认）| schema 租户独立库
	SchemaTemplate string `mapstructure:"schema_template"` // 模式B 库名模板，如 tenant_{code}
	MaxPools       int    `mapstructure:"max_pools"`       // 模式B 租户连接池上限（LRU 淘汰）
	DefaultCode    string `mapstructure:"default_code"`    // 登录未指定租户时的默认租户 code
}

// IDClientConfig devops-id 发号服务接入配置（含客户端预取缓冲）
type IDClientConfig struct {
	Endpoint    string        `mapstructure:"endpoint"`     // devops-id gRPC 地址
	Batch       int           `mapstructure:"batch"`        // 预取批量（1 = 关闭预取，逐次 RPC）
	LowWater    float64       `mapstructure:"low_water"`    // 预取水位（剩余比例）
	DialTimeout time.Duration `mapstructure:"dial_timeout"` // 拨号超时
	CallTimeout time.Duration `mapstructure:"call_timeout"` // 单次调用超时
}
