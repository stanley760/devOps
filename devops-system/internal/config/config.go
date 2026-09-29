// Package config 定义 devops-system 服务的配置结构。
//
// 加载机制复用 devops-infra/config（基础 + APP_ENV 环境覆盖合并），
// 本包只定义自己的根 Config 与专有配置段。
package config

import (
	"time"

	"devops-infra/config"
	securityjwt "devops-infra/security/jwt"
)

// DefaultJWTKeyID key_id 未配置时的默认 kid（token 头与 JWKS 一致）。
const DefaultJWTKeyID = "devops-system-rs256-1"

// Config devops-system 服务根配置，对应 config/config.yaml 顶层结构
type Config struct {
	Server        config.ServerConfig `mapstructure:"server"`
	Log           config.LogConfig    `mapstructure:"log"`
	Mysql         config.MysqlConfig  `mapstructure:"mysql"`
	Redis         config.RedisConfig  `mapstructure:"redis"`
	JWT           JWTConfig           `mapstructure:"jwt"`
	Tenant        TenantConfig        `mapstructure:"tenant"`
	IDClient      IDClientConfig      `mapstructure:"idclient"`
	Nacos         config.NacosConfig  `mapstructure:"nacos"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	APIDoc        APIDocConfig        `mapstructure:"apidoc"`
}

// JWTConfig JWT 双 token 配置。
//
// access token 已从 HS256 升级为 RS256 + kid（存量 HS256 token 升级时整体失效，
// refresh 会话不受影响）。signing_key 语义随之变更为 RSA 私钥 PEM（PKCS#1/PKCS#8），
// 由配置/密钥管理持有；公钥经 JWKS 暴露给各调用方与服务验签。
type JWTConfig struct {
	SigningKey    string        `mapstructure:"signing_key"`    // RS256 私钥 PEM
	KeyID         string        `mapstructure:"key_id"`         // kid 头（轮换标识），缺省 DefaultJWTKeyID
	Issuer        string        `mapstructure:"issuer"`         // 签发者
	AccessExpire  time.Duration `mapstructure:"access_expire"`  // access token 有效期
	RefreshExpire time.Duration `mapstructure:"refresh_expire"` // refresh token（会话）有效期
}

// EffectiveKeyID 返回生效的 kid。
func (c JWTConfig) EffectiveKeyID() string {
	if c.KeyID == "" {
		return DefaultJWTKeyID
	}
	return c.KeyID
}

// SharedJWTConfig 映射为共享 JWT 配置（YAML 键不变，仅做转换）。
// keySet 由调用方注入：服务自验签场景传承载自身公钥的 StaticKeySet，
// 多签发方/轮换场景传 JWKS 后端实现。
func (c JWTConfig) SharedJWTConfig(keySet securityjwt.KeySet) securityjwt.Config {
	return securityjwt.Config{
		PrivateKeyPEM: []byte(c.SigningKey),
		KeyID:         c.EffectiveKeyID(),
		Issuer:        c.Issuer,
		AccessExpire:  c.AccessExpire,
		RefreshExpire: c.RefreshExpire,
		KeySet:        keySet,
	}
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

// ObservabilityConfig 服务级观测配置；ServiceName 由 nacos.registry.service_name 派生。
type ObservabilityConfig struct {
	OTLPEndpoint   string  `mapstructure:"otlp_endpoint"`   // OTLP HTTP 出口；空表示本地 span 不上报
	SamplerRatio   float64 `mapstructure:"sampler_ratio"`   // 根采样率，<=0 视为 1.0
	MetricsEnabled bool    `mapstructure:"metrics_enabled"` // 是否启用 Prometheus 指标
}

// APIDocConfig OpenAPI 文档门控。BasePath 是文档挂载路径，而非 OpenAPI servers URL。
type APIDocConfig struct {
	Enabled  bool   `mapstructure:"enabled"`   // 仅 dev/test 开启，prod 必须关闭
	BasePath string `mapstructure:"base_path"` // 默认 /apidoc
}
