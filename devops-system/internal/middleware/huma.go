// huma 中间件装配胶水（D7）：infra 出决策，本包只做信封呈现与业务注入。
//
// 中间件套件统一经 Kit 装配（kit.go）；本文件的 securityXXX 为各中间件的
// huma 装配函数，信封回调经下方工厂统一呈现。
//
// 文案与状态码逐条对齐历史 gin 版行为：
//   - 认证失败：401「未登录或会话已过期」；会话失效：401「登录状态已失效，请重新登录」
//   - 认证基础设施错误：fail-closed 保持 401 并记录底层错误
//   - 租户不可用：403「租户不可用（停用或已过期）」
//   - 域内鉴权拒绝：403「无操作权限」（含基础设施错误，fail-closed）
//   - 平台非管理员：403「需要平台管理员权限」
package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"devops-infra/http/response"
	securityauthz "devops-infra/security/authz"
	securitymw "devops-infra/security/middleware"
	tenantinfra "devops-infra/tenant"

	"devops-system/internal/auditctx"
	"devops-system/internal/auth"
)

// securityJWT huma 认证中间件：infra 决策 + 本服务信封呈现。
func securityJWT(m *auth.Manager) humaMiddleware {
	authenticate := securitymw.HumaAuthenticate(m, securitymw.HumaOptions{
		OnUnauthorized:        writeHuma401,
		OnInfrastructureError: writeHumaAuthInfraErr,
	})
	return func(hctx huma.Context, next func(huma.Context)) {
		authenticate(hctx, func(authenticated huma.Context) {
			claims := auth.ClaimsFrom(authenticated.Context())
			if claims != nil && claims.Username != "" {
				ctx := auditctx.WithActor(authenticated.Context(), auditctx.Actor{
					Username: claims.Username,
				})
				authenticated = huma.WithContext(authenticated, ctx)
			}
			next(authenticated)
		})
	}
}

// securityTenant 租户校验 + 上下文注入（可用性判定经 Deps 注入 DB 与缓存）。
func securityTenant(deps Deps) humaMiddleware {
	check := newTenantChecker(deps.DB, deps.Cache)
	return tenantinfra.HumaTenantContext(check, writeHuma401, writeHuma403Tenant)
}

// securityPerm 域内鉴权（uid × tid × perm）。
func securityPerm(authorizer securityauthz.Authorizer, perm string) humaMiddleware {
	return securitymw.HumaRequirePermission(authorizer, perm, securitymw.HumaOptions{
		OnUnauthorized:        writeHuma401,
		OnForbidden:           writeHuma403Perm,
		OnInfrastructureError: writeHumaAuthzInfraErr,
	})
}

// securityPlat 平台鉴权（g2 + dom="*"）。
func securityPlat(authorizer securityauthz.Authorizer, perm string) humaMiddleware {
	return securitymw.HumaRequirePlatformPermission(authorizer, perm, securitymw.HumaOptions{
		OnUnauthorized:        writeHuma401,
		OnForbidden:           writeHuma403Platform,
		OnInfrastructureError: writeHumaAuthzInfraErr,
	})
}

// ---- 信封回调工厂 ----

// errMsg 按 errors.Is 匹配错误值并给出对应文案。
type errMsg struct {
	match error
	msg   string
}

// writeEnvelope 固定状态码与文案的信封回调工厂；logKey 非空时先以 slog.Error 记录底层错误。
func writeEnvelope(status int, msg, logKey string) func(huma.Context, error) {
	return func(hctx huma.Context, err error) {
		if logKey != "" {
			slog.Error(logKey, "err", err)
		}
		response.WriteHumaEnvelope(hctx, status, msg)
	}
}

// writeEnvelopeMapped 按 err 值选文案（errors.Is 遍历，wrapped error 亦可命中；默认 defaultMsg）。
func writeEnvelopeMapped(status int, defaultMsg string, cases ...errMsg) func(huma.Context, error) {
	return func(hctx huma.Context, err error) {
		msg := defaultMsg
		for _, c := range cases {
			if errors.Is(err, c.match) {
				msg = c.msg
				break
			}
		}
		response.WriteHumaEnvelope(hctx, status, msg)
	}
}

// 信封回调装配（文案与状态码逐字对齐既有行为）。
var (
	writeHuma401           = writeEnvelopeMapped(http.StatusUnauthorized, "未登录或会话已过期", errMsg{securitymw.ErrSessionInactive, "登录状态已失效，请重新登录"})
	writeHumaAuthInfraErr  = writeEnvelope(http.StatusUnauthorized, "登录状态已失效，请重新登录", "认证基础设施错误")
	writeHuma403Tenant     = writeEnvelope(http.StatusForbidden, "租户不可用（停用或已过期）", "")
	writeHuma403Perm       = writeEnvelope(http.StatusForbidden, "无操作权限", "")
	writeHuma403Platform   = writeEnvelopeMapped(http.StatusForbidden, "无操作权限", errMsg{securitymw.ErrNotPlatformAdmin, "需要平台管理员权限"})
	writeHumaAuthzInfraErr = writeEnvelope(http.StatusForbidden, "无操作权限", "鉴权基础设施错误")
)
