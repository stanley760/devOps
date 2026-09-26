// 租户可用性判定：供 huma 版 HumaTenant 中间件的 check 回调使用（huma.go）。
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

	"devops-system/internal/model"
	"devops-system/internal/store"
)

// tenantCacheTTL 租户状态缓存时长。
const tenantCacheTTL = 5 * time.Minute

// 租户校验与上下文注入已迁 huma（HumaTenant / humaTenantUsable，见 huma.go）；
// gin 版 Tenant() 已删除（P6 零双轨）。以下保留租户可用性判定供 huma 版回调使用。

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
