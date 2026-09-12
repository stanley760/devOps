// Package tenantdb 多租户上下文与数据访问入口。
//
// 模式A（shared）：dao 层统一经 DB(ctx) 获取连接，TenantPlugin（P5 装配）
// 依据 ctx 自动注入/过滤 tenant_id；无租户上下文访问租户表 fail-closed。
//
// 模式B（schema）：DB(ctx) 经 SchemaResolver 按租户 code 解析独立连接池
// （DSN 切库 tenant_{code}）；无租户 ctx 返回主库（t_tenant/casbin/菜单母本）。
package tenantdb

import (
	"context"

	"gorm.io/gorm"

	"devops-system/internal/db"
)

type ctxKey struct{}

// CtxTenant 租户上下文载荷：ID 用于模式A过滤与 casbin 域；Code 用于模式B DSN 切库。
type CtxTenant struct {
	ID   int64
	Code string
}

// With 向 ctx 注入租户（middleware.Tenant 在 JWT 解析后调用）。
func With(ctx context.Context, tenantID int64, tenantCode string) context.Context {
	return context.WithValue(ctx, ctxKey{}, CtxTenant{ID: tenantID, Code: tenantCode})
}

// From 取出租户上下文；第二个返回值表示是否存在。
func From(ctx context.Context) (CtxTenant, bool) {
	t, ok := ctx.Value(ctxKey{}).(CtxTenant)
	return t, ok
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

// DB dao 层统一入口：
//   - 模式A：共享连接 + TenantPlugin 回调过滤
//   - 模式B：SchemaResolver 按租户解析连接池（无租户 ctx 返回主库）
func DB(ctx context.Context) *gorm.DB {
	if r := currentResolver; r != nil {
		return r.resolve(ctx)
	}
	return db.Get().WithContext(ctx)
}
