// Package tenantdb 多租户上下文与数据访问入口。
//
// 模式A（shared）：dao 层统一经 DB(ctx) 获取连接，TenantPlugin（P5 装配）
// 依据 ctx 自动注入/过滤 tenant_id；无租户上下文访问租户表 fail-closed。
package tenantdb

import (
	"context"

	"gorm.io/gorm"

	"devops-system/internal/db"
)

type ctxKey struct{}

// With 向 ctx 注入租户 ID（middleware.Tenant 在 JWT 解析后调用）。
func With(ctx context.Context, tenantID int64) context.Context {
	return context.WithValue(ctx, ctxKey{}, tenantID)
}

// From 取出租户 ID；第二个返回值表示是否存在。
func From(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxKey{}).(int64)
	return id, ok
}

// Skip 显式豁免租户过滤（种子/migrate/平台管理）。
type skipKey struct{}

// WithSkip 标记 ctx 豁免租户隔离（TenantPlugin 直接放行）。
func WithSkip(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipKey{}, true)
}

// IsSkip 是否豁免。
func IsSkip(ctx context.Context) bool {
	_, ok := ctx.Value(skipKey{}).(bool)
	return ok
}

// DB dao 层统一入口（模式A：共享连接 + 回调过滤；模式B 在此按租户解析连接）。
func DB(ctx context.Context) *gorm.DB {
	return db.Get().WithContext(ctx)
}
