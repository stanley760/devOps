// Package auth 认证门面：JWT 双 token 与 Redis 会话管理（C 端形态）。
//
// 签发/校验/会话全部委托共享实现，本包只做C 端的语义适配：
//
//   - devops-infra/security/jwt：RS256 + kid 签发/解析，短期 access token；
//   - devops-infra/security/session：Redis 会话态，每个请求校验会话存在，
//     因此登出/改密/封禁后旧 access 立即失效；
//   - devops-infra/security/middleware：框架无关认证决策（Bearer 解析 → token
//     解析 → 会话态），Authenticate 供中间件消费。
//
// ## 与 devops-system 的差异（C 端有意为之，不是遗漏）
//
// 共享的 session.Store 以 TenantID 划分 Redis 键空间（t{tid}:auth:*）且强制
// TenantID > 0，而 C 端 devops-user 是单租户形态。为复用该实现，本包引入
// 一个**固定哨兵租户 ID**（DefaultTenantID = 1）承载键空间与 claims.tid：
//
//   - 不代表真实租户，仅作键空间隔离与接口占位；
//   - 业务代码不得依据 claims.TenantID 做租户过滤（C 端本就没有租户概念）；
//   - 若将来 C 端引入多租户，届时应新建独立 session.Store 实现而非继续复用哨兵值。
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	securityjwt "github.com/stanley760/devops/devops-infra/security/jwt"
	securitymiddleware "github.com/stanley760/devops/devops-infra/security/middleware"
	securitysession "github.com/stanley760/devops/devops-infra/security/session"

	"github.com/stanley760/devops/devops-user/internal/config"
	"github.com/stanley760/devops/devops-user/internal/refreshtoken"
)

// DefaultTenantID 共享 session.Store 要求的正整数租户 ID（C 端单租户哨兵值）。
//
// 仅用于 Redis 键空间与 claims.tid 占位，**不代表真实租户**。
// 业务代码禁止依据 TenantID 做租户隔离判断。
const DefaultTenantID int64 = 1

// AccessClaims access token 载荷（JSON 标签与 JWT 通用实现对齐）。
type AccessClaims struct {
	UserID     int64  `json:"uid"`
	Username   string `json:"username"`
	SID        string `json:"sid"`
	TenantID   int64  `json:"tid"`
	TenantCode string `json:"tcode"`
	securityjwt.RegisteredClaims
}

// FromShared 共享 claims → 本服务 AccessClaims（中间件边界一次性转换，
// 业务 handler 不依赖基础设施包类型）。
func FromShared(c *securityjwt.Claims) *AccessClaims {
	if c == nil {
		return nil
	}
	return &AccessClaims{
		UserID:           c.UID,
		Username:         c.Username,
		SID:              c.SessionID,
		TenantID:         c.TenantID,
		TenantCode:       c.TenantCode,
		RegisteredClaims: c.RegisteredClaims,
	}
}

// ClaimsFrom 从 context 取 JWT 载荷（JWT 中间件之后可用）。
func ClaimsFrom(ctx context.Context) *AccessClaims {
	return FromShared(securitymiddleware.HumaCurrentClaims(ctx))
}

// Manager JWT + 会话管理器（共享实现门面）。
type Manager struct {
	cfg      config.JWTConfig
	tokens   *securityjwt.Manager
	sessions securitysession.Store
	// refreshTokens 存 refresh token（不透明随机串）。
	refreshTokens *refreshtoken.Store
	authn         *securitymiddleware.Authenticator
}

// New 创建管理器。装配见 cmd/root.go。
//
// refreshTokens 为 nil 时 Refresh 返回 ErrRefreshInvalid（不 nil 解引用），
// 此时 access 的签发与校验仍正常——便于按环境开关 refresh 功能。
func New(cfg config.JWTConfig, tokens *securityjwt.Manager, sessions securitysession.Store, refresh *refreshtoken.Store) *Manager {
	return &Manager{
		cfg:           cfg,
		tokens:        tokens,
		sessions:      sessions,
		refreshTokens: refresh,
		authn:         securitymiddleware.NewAuthenticator(tokens, sessions),
	}
}

// JWKS 输出验签公钥的 JWKS 文档，供 Higress jwt-auth 与其他服务验签。
func (m *Manager) JWKS() (map[string]any, error) {
	return m.tokens.JWKS()
}

// Authenticate 共享认证决策：Bearer 解析 → token 解析 → 会话态校验。
// 中间件消费本方法，故必须满足 securitymw.BearerAuthenticator。
func (m *Manager) Authenticate(ctx context.Context, bearerHeader string) (*securityjwt.Claims, error) {
	return m.authn.Authenticate(ctx, bearerHeader)
}

// Issue 签发 access + refresh 双 token。
//
// refresh 是 **256 位随机不透明串**（服务端只存哈希），**不是 access token**——
// 复用 access 当 refresh 会让攻击者无限续命，且登出/封禁无法真正作废它。
//
// access 的有效期由 jwt.access_expire 决定；refresh 的有效期由
// refreshtoken.Store 的 TTL（= jwt.refresh_expire）决定，二者独立。
func (m *Manager) Issue(ctx context.Context, userID int64, username string) (TokenPair, error) {
	sid := uuid.NewString()
	pair, err := m.tokens.Issue(ctx, securityjwt.Subject{
		UserID:     userID,
		Username:   username,
		TenantID:   DefaultTenantID,
		TenantCode: "c-end",
	}, sid)
	if err != nil {
		return TokenPair{}, err
	}
	if err := m.sessions.Create(ctx, securitysession.Session{
		TenantID:  DefaultTenantID,
		UserID:    userID,
		SessionID: sid,
		TTL:       m.cfg.RefreshExpire,
	}); err != nil {
		return TokenPair{}, err
	}

	plain, hash, err := refreshtoken.Generate()
	if err != nil {
		return TokenPair{}, err
	}
	if err := m.refreshTokens.Save(ctx, hash, userID); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: plain,
		ExpiresIn:    pair.AccessExpiresIn,
		IssuedAt:     time.Now().UnixMilli(),
	}, nil
}

// TokenPair 双 token签发结果。
type TokenPair struct {
	AccessToken  string `json:"access_token" doc:"访问令牌"`
	RefreshToken string `json:"refresh_token" doc:"刷新令牌（不透明随机串，非 JWT）"`
	ExpiresIn    int64  `json:"expires_in" doc:"访问令牌有效期（秒）"`
	IssuedAt     int64  `json:"issued_at" doc:"签发时间（Unix 毫秒）"`
}

// Refresh 用 refresh token 换新双 token（轮换：旧 refresh 立即失效）。
//
// username 由调用方从库里查得后传入——刷新时若不带用户名，新access token 的
// username claim 会是空的，后续任何依赖该claim 的审计/展示都会失真。
//
// 语义：
//   - refresh 不存在 / 已过期 / 已轮换 / 格式非法 → ErrRefreshInvalid
//     （四种情况合并，防探测）；
//   - 轮换是原子的，并发刷新只有一个能成功（见 refreshtoken.Store.Rotate）。
func (m *Manager) Refresh(ctx context.Context, refreshPlain, username string) (TokenPair, error) {
	if m.refreshTokens == nil {
		return TokenPair{}, ErrRefreshInvalid
	}
	oldHash := refreshtoken.Hash(refreshPlain)
	userID, err := m.refreshTokens.Lookup(ctx, oldHash)
	if err != nil {
		return TokenPair{}, ErrRefreshInvalid
	}

	// access 与 refresh 轮换后共享同一 sid：这样轮换 refresh 时不必重建会话，
	// 用户正在使用的会话不会因刷新而被误踢下线。
	sid := uuid.NewString()
	pair, err := m.tokens.Issue(ctx, securityjwt.Subject{
		UserID:     userID,
		Username:   username,
		TenantID:   DefaultTenantID,
		TenantCode: "c-end",
	}, sid)
	if err != nil {
		return TokenPair{}, err
	}

	newPlain, newHash, err := refreshtoken.Generate()
	if err != nil {
		return TokenPair{}, err
	}
	// 原子轮换：查旧 → 删旧 → 写新。失败说明已被并发用掉。
	if err := m.refreshTokens.Rotate(ctx, oldHash, newHash, userID); err != nil {
		return TokenPair{}, ErrRefreshInvalid
	}
	if err := m.sessions.Create(ctx, securitysession.Session{
		TenantID:  DefaultTenantID,
		UserID:    userID,
		SessionID: sid,
		TTL:       m.cfg.RefreshExpire,
	}); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: newPlain,
		ExpiresIn:    pair.AccessExpiresIn,
		IssuedAt:     time.Now().UnixMilli(),
	}, nil
}

// RevokeRefresh 作废指定 refresh token（改密 / 登出时用）。
func (m *Manager) RevokeRefresh(ctx context.Context, refreshPlain string) error {
	if m.refreshTokens == nil || refreshPlain == "" {
		return nil
	}
	return m.refreshTokens.Delete(ctx, refreshtoken.Hash(refreshPlain))
}

// RevokeUserSessions 强制该用户**全端下线**：撤销其全部 refresh + 全部 access 会话。
//
// 组合两侧的撤销能力：
//   - refresh 侧：refreshtoken.RevokeUser（依赖 user → hashes 反查索引）；
//   - access 侧：session.Store.RevokeUser（枚举 sids 集合）。
//
// 两者都是「全端」——用户在手机/平板/PC 上的所有会话同时失效。
//
// ⚠️ 已签发的 access JWT 在被撤销后仍可用到自然过期：本服务每个请求都经
// HumaAuthenticate 校验 Redis 会话态（infra/security/middleware），故撤销后
// **下一次请求即401**，无需等到JWT 过期。这是「有状态校验」带来的好处。
func (m *Manager) RevokeUserSessions(ctx context.Context, userID int64) error {
	var errs []error
	if m.refreshTokens != nil {
		if err := m.refreshTokens.RevokeUser(ctx, userID); err != nil {
			errs = append(errs, err)
		}
	}
	if err := m.sessions.RevokeUser(ctx, DefaultTenantID, userID); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// RevokeUserSessionsExcept 全端下线但保留指定 refresh 对应的设备
// （当前调用方的设备），用于「改密后把其他设备都踢掉」的场景。
//
// keepPlain 为空时等价于 RevokeUserSessions（全踢）。
func (m *Manager) RevokeUserSessionsExcept(ctx context.Context, userID int64, keepPlain string) error {
	var errs []error
	if m.refreshTokens != nil {
		var keepHash string
		if keepPlain != "" {
			keepHash = refreshtoken.Hash(keepPlain)
		}
		if err := m.refreshTokens.RevokeUserExcept(ctx, userID, keepHash); err != nil {
			errs = append(errs, err)
		}
	}
	// access 侧无法按 refresh 反查 sid（session 侧只有 user → sids 集合），
	// 故这里不做 Except——保留设备会同时被踢，需要重登录一次。
	// 若产品要求「保留当前设备」，需给 session 侧补 sid → refreshHash 的反查。
	if err := m.sessions.RevokeUser(ctx, DefaultTenantID, userID); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ErrRefreshInvalid refresh token 无效（不存在 / 已过期 / 已轮换 / 格式非法）。
//
// 三种情况合并为一个错误：区分会让攻击者据此判断某个 token 是否曾经有效。
var ErrRefreshInvalid = errors.New("刷新令牌无效或已过期")

// ErrAccountDisabled 账号已被封禁。
var ErrAccountDisabled = errors.New("账号已被封禁")

// Parse 解析 access token（不校验会话态，仅供需要读载荷的内部逻辑）。
func (m *Manager) Parse(token string) (*AccessClaims, error) {
	claims, err := m.tokens.ParseAccess(token)
	if err != nil {
		return nil, err
	}
	return FromShared(claims), nil
}

// SessionActive 会话是否仍有效（踢下线/改密/封禁后即失效）。
func (m *Manager) SessionActive(ctx context.Context, claims *AccessClaims) bool {
	if claims == nil {
		return false
	}
	active, err := m.sessions.Active(ctx, securitysession.SessionRef{
		TenantID:  DefaultTenantID,
		UserID:    claims.UserID,
		SessionID: claims.SID,
	})
	return err == nil && active
}

// Logout 撤销当前会话。
func (m *Manager) Logout(ctx context.Context, claims *AccessClaims) error {
	if claims == nil {
		return nil
	}
	return m.sessions.Revoke(ctx, securitysession.SessionRef{
		TenantID:  DefaultTenantID,
		UserID:    claims.UserID,
		SessionID: claims.SID,
	})
}
