// Package oauth2 实现 C 端三方登录（GitHub / Google / 微信）的授权码流程。
//
// ## 流程
//
//  1. GET  /user/api/v1/oauth/providers
//     生成 state（防CSRF），拼授权 URL 返回前端；
//  2. 前端跳转到三方页面，用户授权后三方回调
//     GET /user/api/v1/oauth/{provider}/callback?code&state
//     校验 state → 用 code 换 token → 拉三方用户信息；
//  3. 按 (provider, provider_user_id) 查绑定：
//     - 已有绑定 → 签发本地双token；
//     - 无绑定 → 按配置决定「自动建号」或「要求先登录再绑定」。
//
// ## 安全要点
//
//   - **state 必须校验且一次性**：不校验则登录可被 CSRF 劫持
//     （攻击者把自己的 code 塞给受害者登录到自己账号）；
//     不一次性则同一个 state 可被重放。
//   - **provider_user_id 用三方稳定标识**，不用 username/email/nickname——
//     前两者可改可换，微信昵称可重复，用作绑定键会引发错绑与冒用。
//     各家取值：GitHub 数字 ID、Google `sub`、微信 `unionid`（缺失时退 openid）。
//   - **Google / 微信必须校验响应真实性**：Google 走 tokeninfo 校验 aud/iss；
//     微信无 id_token，安全性来自「code 一次性 + 10 分钟过期」+ 全程 HTTPS，
//     故必须校验 openid 非空且显式判 errcode（**微信出错也返回 HTTP 200**）。
//   - 拿到的三方 token 属高敏感凭据，生产须加密入库（见 model/oauth.go）。
//
// ## 依赖边界
//
// 本包只依赖golang.org/x/oauth2 与标准库，**不依赖本项目的 dao/model**，
// 以便独立测试与复用。三方用户信息的解析差异也收敛在 provider.go 内。
//
// ⚠️ 微信的授权端点不是标准 OAuth2 端点（需 #wechat_redirect fragment、
// redirect_uri 单层编码、scope 固定 snsapi_login），故 WeChatProvider
// **不使用** oauth2.Config.AuthCodeURL，手工拼装——详见 provider.go。
package oauth2

import (
	"context"
	"errors"
	"time"
)

// ErrStateInvalid state 无效、过期或已被使用（CSRF 防护失败）。
var ErrStateInvalid = errors.New("state 无效或已过期")

// ErrProviderUnsupported 不支持的三方提供方。
var ErrProviderUnsupported = errors.New("不支持的三方登录提供方")

// ErrExchangeFailed 用授权码换 token 失败（通常是 code 过期或已用）。
var ErrExchangeFailed = errors.New("授权码校验失败")

// UserInfo 三方侧的用户信息（已归一化，跨提供方统一）。
//
// ⚠️ Email 未必已验证：GitHub 的公开邮箱就属于此类。
// **不可用它做账号关联或找回密码**，仅作展示。需邮箱的场景必须走
// 各提供方的「已验证邮箱」接口或让用户自行验证。
type UserInfo struct {
	// ProviderUserID 提供方内的稳定用户标识（绑定键）。
	ProviderUserID string
	Nickname       string
	Email          string // 可能未验证，仅展示用
	AvatarURL      string
}

// Config 单个提供方的配置。
type Config struct {
	// ClientID / ClientSecret 三方应用凭据。Secret 由密钥管理注入，禁止入库。
	ClientID     string
	ClientSecret string
	// RedirectURL 回调地址，须与三方后台登记的完全一致。
	RedirectURL string
	// Scopes 申请的权限范围。
	Scopes []string
	// AutoRegister 未绑定三方账号时是否自动创建本地用户。
	//
	// ⚠️ 自动建号会把「任何能登录该三方的人」变成本地用户。
	// 若产品不希望账号被随意创建，置false：未绑定时返回「请先登录并绑定」，
	// 由已登录用户主动绑定（此时可做「登录用户与三方账号一致性」校验）。
	AutoRegister bool
	// NicknamePrefix 自动建号时昵称前缀，便于识别来源（如 "GitHub:"）。
	NicknamePrefix string
}

// Enabled 该提供方是否可用（凭据齐备）。
func (c Config) Enabled() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

// StateStore state 的存取（防 CSRF）。
//
// 必须「一次性」消费：Verify 成功后立即删除。允许重放等于没有防护。
type StateStore interface {
	// Save 存入 state（携带 state → provider / 跳转后目标地址的映射）。
	Save(ctx context.Context, state string, val StateValue, ttl time.Duration) error
	// Verify 校验并**一次性**取出。无效/已用过都返回 ErrStateInvalid。
	Verify(ctx context.Context, state string) (StateValue, error)
}

// StateValue state 关联的数据。
type StateValue struct {
	Provider string
	// RedirectAfter 登录成功后跳回的前端地址。
	//
	// ⚠️ 必须校验白名单：未校验则攻击者可用 open redirect 钓鱼
	// （跳转到自己的站点偷取 token）。
	RedirectAfter string
	// BindUserID > 0 表示「已登录用户主动绑定三方账号」，回调后绑到该用户。
	BindUserID int64
}
