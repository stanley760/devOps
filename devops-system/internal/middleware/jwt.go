// Package middleware devops-system 业务中间件：JWT / Tenant / Casbin。
//
// 认证与鉴权决策由共享安全包承担，本包只保留 Gin 装配与系统响应信封：
//
//	JWT     → devops-infra/security/middleware.Authenticator（Bearer 解析 →
//	          RS256 解析 → Redis 会话态），401 文案保持原状
//	Tenant  → 租户状态校验（业务关注点，留在本服务）
//	Perm/PlatformPerm → authz 门面（路由到共享 Casbin authorizer，fail-closed）
//
// 统一中间件链：Recovery → AccessLog（CORS 由 Higress 网关统一处理）
//
//	├─ 公开：/auth/login /auth/refresh
//	└─ JWT → Tenant(租户校验) → [CasbinAuth(P5)] → 业务路由
//	└─ JWT → Platform(g2 校验，P5) → 租户管理路由
package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"devops-infra/http/response"
	securitymiddleware "devops-infra/security/middleware"

	"devops-system/internal/auth"
)

// ginKeys gin.Context 内传递的键。
const (
	KeyClaims = "auth_claims"
)

// CurrentClaims 从 gin.Context 取 JWT 载荷（JWT 中间件之后可用）。
func CurrentClaims(c *gin.Context) *auth.AccessClaims {
	v, _ := c.Get(KeyClaims)
	claims, _ := v.(*auth.AccessClaims)
	return claims
}

// JWT 共享认证决策 + 系统响应信封：
// 认证失败（缺失头/非法 token/签名不过）→ 401「未登录或会话已过期」；
// 会话失效（logout/改密/停用踢下线）→ 401「登录状态已失效，请重新登录」；
// 基础设施错误（Redis 故障等）fail-closed，保持 401 并记录底层错误。
func JWT(m *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		shared, err := m.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
		if err != nil {
			switch {
			case errors.Is(err, securitymiddleware.ErrInfrastructure):
				slog.Error("认证基础设施错误", "err", err, "path", c.Request.URL.Path)
				response.Fail(c, http.StatusUnauthorized, "登录状态已失效，请重新登录")
			case errors.Is(err, securitymiddleware.ErrSessionInactive):
				response.Fail(c, http.StatusUnauthorized, "登录状态已失效，请重新登录")
			default:
				response.Fail(c, http.StatusUnauthorized, "未登录或会话已过期")
			}
			c.Abort()
			return
		}

		c.Set(KeyClaims, auth.FromShared(shared))
		c.Next()
	}
}
