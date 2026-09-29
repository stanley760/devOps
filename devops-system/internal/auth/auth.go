// Package auth 认证门面：JWT 双 token 与 Redis 会话管理。
//
// 签发/校验与会话状态分别委托共享实现（本包不再持有安全算法细节）：
//   - devops-infra/security/jwt：RS256 + kid 签发/解析，claims 携带
//     uid/username/sid/tid/tcode，短期（access_expire）。存量 HS256 token
//     升级时整体失效，refresh 会话不受影响。
//   - devops-infra/security/session：Redis 会话态（键空间租户分段，键形不变）：
//     t{tid}:auth:refresh:{sid} = user_id，TTL = refresh_expire
//     t{tid}:auth:sids:{uid}    = SET<sid>（RevokeAllSessions 用）
//     auth:sid2uid:{sid}        = "tid:uid"（refresh 端点由 sid 定位会话归属）
//   - devops-infra/security/middleware：框架无关认证决策（Bearer 解析 →
//     token 解析 → 会话态），Authenticate 供 Gin 中间件消费。
//
//   - 每个请求校验会话存在 —— 踢下线（logout/改密/停用）后旧 access 立即 401
//   - refresh 端点轮换 sid（先建新会话再撤销旧 sid），对齐 rotation 最佳实践
package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"devops-infra/errs"
	securityjwt "devops-infra/security/jwt"
	securitymiddleware "devops-infra/security/middleware"
	securitysession "devops-infra/security/session"

	"devops-system/internal/config"
)

// AccessClaims access token 载荷（字段名与 JSON 标签保持兼容，调用方无感）。
type AccessClaims struct {
	UserID     int64  `json:"uid"`
	Username   string `json:"username"`
	SID        string `json:"sid"`
	TenantID   int64  `json:"tid"`
	TenantCode string `json:"tcode"`
	securityjwt.RegisteredClaims
}

// FromShared 共享 claims → 系统 AccessClaims（中间件边界一次性转换，
// 业务 handler 不依赖基础设施包）。
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

// ClaimsFrom 从 context 取 JWT 载荷（HumaJWT 之后可用）。
// 业务 handler 只面对 AccessClaims，不依赖基础设施 claims 类型。
func ClaimsFrom(ctx context.Context) *AccessClaims {
	return FromShared(securitymiddleware.HumaCurrentClaims(ctx))
}

// Manager JWT + 会话管理器（共享实现门面）。
type Manager struct {
	cfg        config.JWTConfig
	tokens     *securityjwt.Manager
	sessions   securitysession.Store
	authn      *securitymiddleware.Authenticator
	refreshTTL time.Duration
}

// New 创建管理器：注入共享 JWT 管理器与会话存储（装配见 cmd/root.go）。
func New(cfg config.JWTConfig, tokens *securityjwt.Manager, sessions securitysession.Store) *Manager {
	return &Manager{
		cfg:        cfg,
		tokens:     tokens,
		sessions:   sessions,
		authn:      securitymiddleware.NewAuthenticator(tokens, sessions),
		refreshTTL: cfg.RefreshExpire,
	}
}

// JWKS 输出验签公钥的 JWKS 文档（{"keys":[...]}），供 Higress/服务拉取。
func (m *Manager) JWKS() (map[string]any, error) {
	return m.tokens.JWKS()
}

// Authenticate 共享认证决策：Bearer 解析 → token 解析 → 会话态校验。
// 返回类型化错误（ErrUnauthorized/ErrSessionInactive/ErrInfrastructure）。
func (m *Manager) Authenticate(ctx context.Context, bearerHeader string) (*securityjwt.Claims, error) {
	return m.authn.Authenticate(ctx, bearerHeader)
}

// Issue 双 token 签发（登录 / refresh 轮换共用）。
// 返回 access token、refresh token（即 sid）与 access 有效期秒数。
func (m *Manager) Issue(ctx context.Context, userID int64, username string, tenantID int64, tenantCode string) (access, refresh string, expiresIn int64, err error) {
	sid := uuid.NewString()
	pair, err := m.tokens.Issue(ctx, securityjwt.Subject{
		UserID: userID, Username: username, TenantID: tenantID, TenantCode: tenantCode,
	}, sid)
	if err != nil {
		return "", "", 0, errs.Wrap(err, errs.CodeInternal, "签发 token 失败")
	}
	if err := m.sessions.Create(ctx, securitysession.Session{
		TenantID: tenantID, UserID: userID, SessionID: sid, TTL: m.refreshTTL,
	}); err != nil {
		return "", "", 0, errs.Wrap(err, errs.CodeInternal, "写入会话失败")
	}
	return pair.AccessToken, sid, pair.AccessExpiresIn, nil
}

// Parse 校验并解析 access token（不查 Redis，会话态由中间件另行校验）。
func (m *Manager) Parse(token string) (*AccessClaims, error) {
	shared, err := m.tokens.ParseAccess(token)
	if err != nil {
		return nil, errs.New(errs.CodeUnauthorized, "未登录或会话已过期")
	}
	return FromShared(shared), nil
}

// SessionActive 会话是否仍有效（logout/踢下线/改密后立即失效的关键）。
func (m *Manager) SessionActive(ctx context.Context, claims *AccessClaims) bool {
	active, err := m.sessions.Active(ctx, securitysession.SessionRef{
		TenantID: claims.TenantID, UserID: claims.UserID, SessionID: claims.SID,
	})
	return err == nil && active
}

// LookupSID 由 refresh token（sid）定位会话归属（refresh 端点用）。
func (m *Manager) LookupSID(ctx context.Context, sid string) (tenantID, userID int64, ok bool) {
	return m.sessions.Lookup(ctx, sid)
}

// SIDMatch 校验 sid 会话确属该用户。
func (m *Manager) SIDMatch(ctx context.Context, tenantID int64, sid string, userID int64) bool {
	matched, err := m.sessions.Matches(ctx, securitysession.SessionRef{
		TenantID: tenantID, UserID: userID, SessionID: sid,
	}, userID)
	return err == nil && matched
}

// Rotate 轮换会话：签发新双 token 并撤销旧 sid（service.Refresh 校验通过后调用）。
// 先建新会话再撤销旧会话；撤销失败仅记日志（旧会话自然过期，不阻断新会话使用）。
func (m *Manager) Rotate(ctx context.Context, oldSID string, userID int64, username string, tenantID int64, tenantCode string) (access, refresh string, expiresIn int64, err error) {
	access, refresh, expiresIn, err = m.Issue(ctx, userID, username, tenantID, tenantCode)
	if err != nil {
		return "", "", 0, err
	}
	if err := m.sessions.Revoke(ctx, securitysession.SessionRef{
		TenantID: tenantID, UserID: userID, SessionID: oldSID,
	}); err != nil {
		slog.Error("轮换撤销旧会话失败", "err", err, "tid", tenantID, "uid", userID)
	}
	return access, refresh, expiresIn, nil
}

// Logout 撤销当前会话。
func (m *Manager) Logout(ctx context.Context, claims *AccessClaims) {
	if err := m.sessions.Revoke(ctx, securitysession.SessionRef{
		TenantID: claims.TenantID, UserID: claims.UserID, SessionID: claims.SID,
	}); err != nil {
		slog.Error("登出撤销会话失败", "err", err, "tid", claims.TenantID, "uid", claims.UserID)
	}
}

// RevokeAllSessions 撤销用户全部会话（改密/重置密码/停用/删除用户、停用租户）。
func (m *Manager) RevokeAllSessions(ctx context.Context, tenantID, userID int64) {
	if err := m.sessions.RevokeUser(ctx, tenantID, userID); err != nil {
		slog.Error("撤销用户会话失败", "err", err, "tid", tenantID, "uid", userID)
	}
}

// RevokeTenantSessions 撤销租户全部会话（停用租户 → 全员下线）。
func (m *Manager) RevokeTenantSessions(ctx context.Context, tenantID int64) {
	if err := m.sessions.RevokeTenant(ctx, tenantID); err != nil {
		slog.Error("撤销租户会话失败", "err", err, "tid", tenantID)
	}
}
