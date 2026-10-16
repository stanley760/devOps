// Package config 定义 devops-user（C 端用户服务）的配置结构。
//
// 加载机制复用 devops-infra/config（bootstrap 基础 + APP_ENV 环境覆盖合并 +
// 可选 Nacos 远程配置），本包只声明自己的根Config 与专有配置段。
//
// 与 devops-system 的差异（有意为之，不是遗漏）：
//   - 无 tenant 段：C 端是单租户形态，不做多租户隔离；
//   - 无 authz/casbin：C 端权限是「只能操作自己的数据」，属行级归属校验，
//     由 service 层按 claims.UID 过滤，不引入 RBAC；
//   - jwt.issuer 独立（本服务默认 devops-user），避免与 devops-system 的 token
//     在网关 jwt-auth 的 consumers 里互相匹配。
package config

import (
	"time"

	"github.com/stanley760/devops/devops-infra/config"
	"github.com/stanley760/devops/devops-infra/security/gateway"
	securityjwt "github.com/stanley760/devops/devops-infra/security/jwt"
)

// DefaultJWTKeyID key_id 未配置时的默认 kid（token 头与 JWKS 一致）。
const DefaultJWTKeyID = "devops-user-rs256-1"

// Config devops-user 服务根配置，对应 config/bootstrap.yaml 顶层结构。
type Config struct {
	Server        config.ServerConfig `mapstructure:"server"`
	Log           config.LogConfig    `mapstructure:"log"`
	Mysql         config.MysqlConfig  `mapstructure:"mysql"`
	Redis         config.RedisConfig  `mapstructure:"redis"`
	JWT           JWTConfig           `mapstructure:"jwt"`
	IDClient      IDClientConfig      `mapstructure:"idclient"`
	Nacos         config.NacosConfig  `mapstructure:"nacos"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	APIDoc        APIDocConfig        `mapstructure:"apidoc"`
	Gateway       gateway.Config      `mapstructure:"gateway"`
	LoginLock     LoginLockConfig     `mapstructure:"login_lock"`
	OAuth         OAuthConfig         `mapstructure:"oauth"`
}

// OAuthConfig 三方登录配置。
//
// ⚠️ client_secret 必须由密钥管理注入，**禁止提交真实值**；
// dev 下留空即可（对应提供方显示为「未配置」，前端隐藏入口）。
type OAuthConfig struct {
	// StateTTL state 有效期；超过 10 分钟的授权请求基本是用户放着不管，
	// 再放开只会增大重放窗口。
	StateTTL time.Duration `mapstructure:"state_ttl"`
	// RedirectWhitelist 登录后跳回地址的白名单前缀。
	//
	// ⚠️ **必须配**：不校验则攻击者可用 open redirect 钓鱼
	//（登录成功后跳转到自己的站点偷 token）。
	RedirectWhitelist []string `mapstructure:"redirect_whitelist"`
	// Github / Google / Wechat 各提供方配置。
	Github ProviderConfig `mapstructure:"github"`
	Google ProviderConfig `mapstructure:"google"`
	Wechat ProviderConfig `mapstructure:"wechat"`
}

// ProviderConfig 单个三方提供方配置。
type ProviderConfig struct {
	ClientID     string   `mapstructure:"client_id"`
	ClientSecret string   `mapstructure:"client_secret"`
	RedirectURL  string   `mapstructure:"redirect_url"`
	// Scopes 申请的权限范围。
	//
	// ⚠️ **微信忽略此项**：网站应用只允许 `snsapi_login`，配别的值也会被
	// provider 内部强制覆盖为 snsapi_login。保留字段仅为与其他提供方结构一致。
	Scopes []string `mapstructure:"scopes"`
	// AutoRegister 未绑定时是否自动建本地号。
	//
	// ⚠️ 自动建号会把「任何能登录该三方的人」变成本地用户。
	// 面向公开注册的产品通常置 false：先登录再绑定，可在绑定时做一致性校验。
	AutoRegister bool `mapstructure:"auto_register"`
	// NicknamePrefix 自动建号时的昵称前缀（便于识别来源）。
	NicknamePrefix string `mapstructure:"nickname_prefix"`
}

// LoginLockConfig 登录失败限流与账号临时锁定配置。
//
// bcrypt 只能拖慢单次爆破，无法阻止高速尝试；C 端开放注册、用户名可枚举，
// 必须叠加 IP 限流 + 账号锁定两个维度（缺一可被绕过）。
type LoginLockConfig struct {
	// Enabled 总开关；false 时不做限流与锁定（dev/测试可关）。
	Enabled bool `mapstructure:"enabled"`
	// MaxIPFails 单 IP 在窗口内允许的最大失败次数；0 表示不限 IP。
	MaxIPFails int `mapstructure:"max_ip_fails"`
	// IPWindow IP 计数窗口。
	IPWindow time.Duration `mapstructure:"ip_window"`
	// MaxUserFails 单账号在窗口内允许的最大失败次数；0 表示不锁账号。
	MaxUserFails int `mapstructure:"max_user_fails"`
	// UserWindow 账号失败计数窗口。
	UserWindow time.Duration `mapstructure:"user_window"`
	// LockTTL 账号锁定时长。
	LockTTL time.Duration `mapstructure:"lock_ttl"`
}

// JWTConfig JWT 双token 配置（RS256）。
//
// 与 devops-system 同构但**必须使用不同的 issuer 与 kid**：两个服务都用同一套
// jwt-auth consumer 时，issuer 相同会导致 token 跨服务互认——B 端 token 能访问
// C 端接口，反之亦然。生产各自持私钥，仅公钥经 JWKS 暴露给网关验签。
type JWTConfig struct {
	SigningKey    string        `mapstructure:"signing_key"`    // RS256 私钥 PEM（PKCS#1/PKCS#8）
	KeyID         string        `mapstructure:"key_id"`         // kid 头（轮换标识），缺省 DefaultJWTKeyID
	Issuer        string        `mapstructure:"issuer"`         // 签发者，须与网关 jwt-auth 的 consumers[].issuer 一致
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
// keySet 由调用方注入：本服务自验签场景传承载自身公钥的 StaticKeySet。
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

// IDClientConfig devops-id 发号服务接入配置（含客户端预取缓冲）。
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
