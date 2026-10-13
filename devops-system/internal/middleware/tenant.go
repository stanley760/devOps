// 租户可用性判定：供 Kit 的租户中间件（securityTenant，huma.go）使用。
//
// 状态校验带 Redis 缓存（tenant:{id}，5min）：
// 即时失效由「停用租户时 RevokeAllSessions」保证（停用 → 全员 401），
// 缓存仅避免每个请求回源查库。
package middleware

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-infra/cache"

	"github.com/stanley760/devOps/devops-system/internal/model"
)

// tenantCacheTTL 租户状态缓存时长。
const tenantCacheTTL = 5 * time.Minute

// newTenantChecker 租户可用性判定闭包工厂：DB 与缓存经依赖注入（不再依赖全局单例）。
// 返回 true 表示租户存在、未停用、未过期；结果缓存 5min。
func newTenantChecker(gdb *gorm.DB, c cache.Cache) func(ctx context.Context, tenantID int64) bool {
	return func(ctx context.Context, tenantID int64) bool {
		cacheKey := fmt.Sprintf("tenant:%d", tenantID)
		if c != nil {
			if value, err := c.Get(ctx, cacheKey); err == nil && value == "ok" {
				return true
			}
		}
		if gdb == nil {
			return false // fail-closed：未注入 DB（测试/装配缺失）时拒绝
		}

		var t model.Tenant
		if err := gdb.WithContext(ctx).Where("id = ?", tenantID).First(&t).Error; err != nil {
			return false
		}
		if t.Status != 1 { // 停用
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
}
