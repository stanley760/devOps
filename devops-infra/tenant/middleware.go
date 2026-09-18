package tenant

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const DefaultHeader = "X-Tenant-ID"

// Middleware 从请求头注入租户上下文。
//
// required 为 true 时，缺失或非法的租户头会直接返回 400；
// 为 false 时请求可以在无租户上下文下继续，但所有受保护的数据库查询
// 仍会由 Scope 返回 ErrTenantNotFound。
func Middleware(required bool) gin.HandlerFunc {
	return MiddlewareWithHeader(DefaultHeader, required)
}

// MiddlewareWithHeader 使用指定请求头注入租户上下文。
func MiddlewareWithHeader(header string, required bool) gin.HandlerFunc {
	if header == "" {
		header = DefaultHeader
	}
	return func(c *gin.Context) {
		raw := c.GetHeader(header)
		if raw == "" {
			if required {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "缺少租户编号"})
				return
			}
			c.Next()
			return
		}

		tenantID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || tenantID <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "租户编号必须是正整数"})
			return
		}
		ctx, err := WithTenant(c.Request.Context(), tenantID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "租户编号无效"})
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
