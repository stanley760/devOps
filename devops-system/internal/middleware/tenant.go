// Tenant 中间件：JWT 之后，claims → ctx 注入租户 + 租户状态校验。
//
// 状态校验带 Redis 缓存（tenant:{id}，5min）：
// 即时失效由「停用租户时 RevokeAllSessions」保证（停用 → 全员 401），
// 缓存仅避免每个请求回源查库。
package middleware

import (
	"context"
	client "devops-infra/database/mysql"
	"devops-infra/http/response"
	"devops-system/internal/model"
	"devops-system/internal/store"
	infratenant "devops-infra/tenant"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"time"
)

// tenantCacheTTL 租户状态缓存时长。
const tenantCacheTTL = 5 * time.Minute

// Tenant 注入租户上下文并校验租户可用（JWT 之后挂载）。
func Tenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := CurrentClaims(c)
		if claims == nil {
			response.Fail(c, http.StatusUnauthorized, "未登录或会话已过期")
			c.Abort()
			return
		}

		if !tenantUsable(c.Request.Context(), client.Get(), claims.TenantID) {
			response.Fail(c, http.StatusForbidden, "租户不可用（停用或已过期）")
			c.Abort()
			return
		}

		// ctx 注入租户：dao 层经 tenantdb.DB(ctx) 消费（模式A 插件过滤 / 模式B 切库）
		ctx := infratenant.With(c.Request.Context(), claims.TenantID, claims.TenantCode)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// tenantUsable 租户存在、未停用、未过期；结果缓存 5min。
func tenantUsable(ctx context.Context, gdb *gorm.DB, tenantID int64) bool {
	cacheKey := fmt.Sprintf("tenant:%d", tenantID)
	c := store.Cache()
	if c != nil {
		if value, err := c.Get(ctx, cacheKey); err == nil && value == "ok" {
			return true
		}
	}

	var t model.Tenant
	err := gdb.WithContext(ctx).Where("id = ?", tenantID).First(&t).Error
	if err != nil {
		return false
	}
	if t.Status != 0 { // 停用
		return false
	}
	if t.ExpireTime != nil && t.ExpireTime.Before(time.Now()) { // 过期
		return false
	}

	if c != nil {
		_ = c.Set(ctx, cacheKey, "ok", tenantCacheTTL)
	}
	return true
}
