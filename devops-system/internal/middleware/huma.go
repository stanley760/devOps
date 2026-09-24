// huma 中间件装配胶水（D7）：infra 出决策，本包只做信封呈现与业务注入。
//
// 与 gin 版中间件（jwt.go/tenant.go/casbin.go）在迁移过渡期并行存在；
// P6 清理后 gin 版删除，本文件成为唯一中间件出口。
//
// 文案与状态码逐条对齐 gin 版：
//   - 认证失败：401「未登录或会话已过期」；会话失效：401「登录状态已失效，请重新登录」
//   - 认证基础设施错误：fail-closed 保持 401 并记录底层错误
//   - 租户不可用：403「租户不可用（停用或已过期）」
//   - 域内鉴权拒绝：403「无操作权限」（含基础设施错误，对齐 gin 版 fail-closed）
//   - 平台非管理员：403「需要平台管理员权限」
package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"devops-infra/http/response"
	client "devops-infra/database/mysql"
	securitymw "devops-infra/security/middleware"
	tenantinfra "devops-infra/tenant"

	"devops-system/internal/auth"
	"devops-system/internal/authz"
)

// ClaimsFrom 从 context 取 JWT 载荷（HumaJWT 之后可用）。
// 业务 handler 只面对 auth.AccessClaims（经 auth.FromShared 边界转换），不依赖基础设施 claims 类型。
func ClaimsFrom(ctx context.Context) *auth.AccessClaims {
	return auth.FromShared(securitymw.HumaCurrentClaims(ctx))
}

// HumaJWT huma 认证中间件：infra 决策 + 本服务信封呈现。
func HumaJWT(m *auth.Manager) func(huma.Context, func(huma.Context)) {
	return securitymw.HumaAuthenticate(m, securitymw.HumaOptions{
		OnUnauthorized:        writeHuma401,
		OnInfrastructureError: writeHumaAuthInfraErr,
	})
}

// HumaTenant 租户校验 + 上下文注入（复用本包现有 tenantUsable：DB 状态 + Redis 缓存）。
func HumaTenant() func(huma.Context, func(huma.Context)) {
	return tenantinfra.HumaTenantContext(humaTenantUsable, writeHuma401, writeHuma403Tenant)
}

// HumaPerm 域内鉴权（uid × tid × perm）。
func HumaPerm(perm string) func(huma.Context, func(huma.Context)) {
	return securitymw.HumaRequirePermission(authz.Authorizer(), perm, securitymw.HumaOptions{
		OnUnauthorized:        writeHuma401,
		OnForbidden:           writeHuma403Perm,
		OnInfrastructureError: writeHumaAuthzInfraErr,
	})
}

// HumaPlatformPerm 平台鉴权（g2 + dom="*"）。
func HumaPlatformPerm(perm string) func(huma.Context, func(huma.Context)) {
	return securitymw.HumaRequirePlatformPermission(authz.Authorizer(), perm, securitymw.HumaOptions{
		OnUnauthorized:        writeHuma401,
		OnForbidden:           writeHuma403Platform,
		OnInfrastructureError: writeHumaAuthzInfraErr,
	})
}

// ---- 业务注入与信封回调 ----

// humaTenantUsable 适配 infra 租户 check 回调签名。
func humaTenantUsable(ctx context.Context, tenantID int64) bool {
	return tenantUsable(ctx, client.Get(), tenantID)
}

// writeHuma401 认证失败信封（文案对齐 gin 版 jwt.go）。
func writeHuma401(hctx huma.Context, err error) {
	msg := "未登录或会话已过期"
	if errors.Is(err, securitymw.ErrSessionInactive) {
		msg = "登录状态已失效，请重新登录"
	}
	response.WriteHumaEnvelope(hctx, http.StatusUnauthorized, msg)
}

// writeHumaAuthInfraErr 认证基础设施错误：fail-closed 保持 401 并记录底层错误。
func writeHumaAuthInfraErr(hctx huma.Context, err error) {
	slog.Error("认证基础设施错误", "err", err)
	response.WriteHumaEnvelope(hctx, http.StatusUnauthorized, "登录状态已失效，请重新登录")
}

// writeHuma403Tenant 租户不可用。
func writeHuma403Tenant(hctx huma.Context, _ error) {
	response.WriteHumaEnvelope(hctx, http.StatusForbidden, "租户不可用（停用或已过期）")
}

// writeHuma403Perm 域内鉴权拒绝。
func writeHuma403Perm(hctx huma.Context, _ error) {
	response.WriteHumaEnvelope(hctx, http.StatusForbidden, "无操作权限")
}

// writeHuma403Platform 平台鉴权拒绝：区分非平台管理员与 Enforce 拒绝。
func writeHuma403Platform(hctx huma.Context, err error) {
	if errors.Is(err, securitymw.ErrNotPlatformAdmin) {
		response.WriteHumaEnvelope(hctx, http.StatusForbidden, "需要平台管理员权限")
		return
	}
	response.WriteHumaEnvelope(hctx, http.StatusForbidden, "无操作权限")
}

// writeHumaAuthzInfraErr 鉴权基础设施错误：fail-closed 按 403 拒绝（对齐 gin 版行为）。
func writeHumaAuthzInfraErr(hctx huma.Context, err error) {
	slog.Error("鉴权基础设施错误", "err", err)
	response.WriteHumaEnvelope(hctx, http.StatusForbidden, "无操作权限")
}
