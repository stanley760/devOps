// Casbin 鉴权中间件：路由声明式权限标识 + 域模型 Enforce。
//
//	业务路由：JWT → Tenant → Perm("system:user:query")        域内鉴权（dom = tenant_id）
//	平台路由：JWT → PlatformPerm("platform:tenant:query")     g2 平台角色 + dom = "*"
//
// PlatformPerm 先校验 g2 平台管理员再 Enforce（dom="*"），
// 租户超管 p, super_admin, {tid}, * 因 dom 不匹配无法触达平台接口。
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"devops-infra/http/response"

	"devops-system/internal/authz"
)

// Perm 域内权限：校验用户在本租户是否持有权限标识。
func Perm(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := CurrentClaims(c)
		if claims == nil {
			response.Fail(c, http.StatusUnauthorized, "未登录或会话已过期")
			c.Abort()
			return
		}
		if !authz.Enforce(claims.UserID, claims.TenantID, perm) {
			response.Fail(c, http.StatusForbidden, "无操作权限")
			c.Abort()
			return
		}
		c.Next()
	}
}

// PlatformPerm 平台权限：先挡非平台管理员（g2），再以 dom="*" Enforce。
func PlatformPerm(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := CurrentClaims(c)
		if claims == nil {
			response.Fail(c, http.StatusUnauthorized, "未登录或会话已过期")
			c.Abort()
			return
		}
		if !authz.IsPlatformAdmin(claims.UserID) {
			response.Fail(c, http.StatusForbidden, "需要平台管理员权限")
			c.Abort()
			return
		}
		if !authz.EnforcePlatform(claims.UserID, perm) {
			response.Fail(c, http.StatusForbidden, "无操作权限")
			c.Abort()
			return
		}
		c.Next()
	}
}
