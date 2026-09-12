// Package middleware devops-system 业务中间件：JWT / Tenant（方案 6.2）。
//
// 统一中间件链：Recovery → AccessLog → CORS（infra httpx）
//
//	├─ 公开：/auth/login /auth/refresh
//	└─ JWT → Tenant(租户校验) → [CasbinAuth(P5)] → 业务路由
//	└─ JWT → Platform(g2 校验，P5) → 租户管理路由
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"devops-infra/response"

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

// JWT 解析 Authorization: Bearer <access> → 校验 Redis 会话态 → 注入 claims。
// 会话被撤销（logout/改密/停用）时立即 401。
func JWT(m *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			response.Fail(c, http.StatusUnauthorized, "未登录或会话已过期")
			c.Abort()
			return
		}

		claims, err := m.Parse(token)
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, "未登录或会话已过期")
			c.Abort()
			return
		}
		if !m.SessionActive(c.Request.Context(), claims) {
			response.Fail(c, http.StatusUnauthorized, "登录状态已失效，请重新登录")
			c.Abort()
			return
		}

		c.Set(KeyClaims, claims)
		c.Next()
	}
}
