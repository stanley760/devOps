// Package database 定义跨数据库适配器的事务语义。
//
// 业务层只依赖 Transaction / ExecutorFromContext，不感知 MySQL DSN、
// 连接池或 GORM Transaction 的参数细节。
package database

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
)

// Executor 是可开启事务的数据库执行器。
// *gorm.DB 天然满足该接口，测试中也可以用自定义 fake 替换。
type Executor interface {
	Transaction(fc func(tx *gorm.DB) error, opts ...*sql.TxOptions) error
}

// TxFunc 事务函数。fn 收到的 context 已携带当前事务，
// Repository 通过 ExecutorFromContext 取出并复用同一个事务。
type TxFunc func(ctx context.Context) error

type txContextKey struct{}

// Transaction 在 executor 上执行 fn，并把事务注入 context。
// 事务边界由 service 层控制，Repository 不再自行 Begin/Commit/Rollback。
func Transaction(ctx context.Context, executor Executor, fn TxFunc) error {
	if executor == nil {
		return errors.New("database: executor is nil")
	}
	if fn == nil {
		return errors.New("database: transaction function is nil")
	}

	return executor.Transaction(func(tx *gorm.DB) error {
		if tx == nil {
			return errors.New("database: transaction is nil")
		}
		return fn(context.WithValue(ctx, txContextKey{}, tx.WithContext(ctx)))
	})
}

// ExecutorFromContext 取出当前事务；不存在时返回 false。
func ExecutorFromContext(ctx context.Context) (*gorm.DB, bool) {
	tx, ok := ctx.Value(txContextKey{}).(*gorm.DB)
	return tx, ok && tx != nil
}
