// Package tenantdb 单库多租户上下文与数据访问入口。
//
// dao 层统一经 DB(ctx) 获取连接，TenantPlugin 依据 ctx 自动注入/过滤
// tenant_id；无租户上下文访问租户表 fail-closed。
package tenantdb

import (
	"context"

	"gorm.io/gorm"

	database "github.com/stanley760/devOps/devops-infra/database"
	client "github.com/stanley760/devOps/devops-infra/database/mysql"
)

// DB dao 层统一入口：
// 所有租户共用同一连接，租户隔离由 TenantPlugin 的 tenant_id 过滤实现。
func DB(ctx context.Context) *gorm.DB {
	if tx, ok := database.ExecutorFromContext(ctx); ok {
		return tx
	}
	return client.Get().WithContext(ctx)
}

// Transaction 在共享数据库上执行事务。
// 事务会注入 context，DB(ctx) 内的 Repository 自动复用同一个事务。
func Transaction(ctx context.Context, fn database.TxFunc) error {
	if _, ok := database.ExecutorFromContext(ctx); ok {
		return fn(ctx)
	}
	return database.Transaction(ctx, DB(ctx), fn)
}
