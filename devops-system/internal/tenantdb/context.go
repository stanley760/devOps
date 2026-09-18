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

	database "devops-infra/database"
	client "devops-infra/database/mysql"
)

// DB dao 层统一入口：
//   - 模式A：共享连接 + TenantPlugin 回调过滤
//   - 模式B：SchemaResolver 按租户解析连接池（无租户 ctx 返回主库）
func DB(ctx context.Context) *gorm.DB {
	if tx, ok := database.ExecutorFromContext(ctx); ok {
		return tx
	}
	if r := currentResolver; r != nil {
		return r.resolve(ctx)
	}
	return client.Get().WithContext(ctx)
}

// Transaction 在当前租户对应的数据库上执行事务。
// 事务会注入 context，DB(ctx) 内的 Repository 自动复用同一个事务。
func Transaction(ctx context.Context, fn database.TxFunc) error {
	if _, ok := database.ExecutorFromContext(ctx); ok {
		return fn(ctx)
	}
	return database.Transaction(ctx, DB(ctx), fn)
}
