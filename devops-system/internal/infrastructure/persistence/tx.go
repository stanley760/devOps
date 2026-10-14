// Package persistence 是领域仓储端口的 GORM 实现（基础设施层）。
//
// 依赖方向：infrastructure → domain。领域层不认识本包，反之亦然不成立。
package persistence

import (
	"context"

	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-system/internal/domain/shared"
	"github.com/stanley760/devops/devops-system/internal/tenantdb"
)

// txKey 事务句柄在 context 中的存放键。
type txKey struct{}

// gormTxManager 是 shared.TxManager 的 GORM 实现。
//
// 解决的问题：改造前 dao.WithTx 把 *gorm.DB 完全封在 dao 包内，
// service 层无法声明跨聚合用例的事务边界，只能在应用层写手工补偿
// （service/tenant.go:104 用 HardDeleteTenant 回滚租户行）。
// 现在应用层通过本端口声明边界，事务句柄经 ctx 传递给各仓储实现，
// *gorm.DB 不再泄漏出 persistence 包。
type gormTxManager struct{}

// NewTxManager 构造事务管理器。
func NewTxManager() shared.TxManager { return gormTxManager{} }

// WithinTx 在单一事务内执行 fn。
//
// 若 ctx 中已存在事务句柄，则直接复用（嵌套调用不额外开事务），
// 使仓储内部调用 WithinTx 时不会产生事务嵌套导致的锁问题。
func (gormTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := txFrom(ctx); ok {
		// 已在事务中：复用外层事务。
		return fn(ctx)
	}
	return tenantdb.DB(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// txFrom 从 ctx 取事务句柄。
//
// 仓储实现统一经此取连接：有事务则复用，无事务则回落到租户连接
// （tenantdb.DB(ctx) 会应用租户过滤插件）。
func txFrom(ctx context.Context) (*gorm.DB, bool) {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok && tx != nil {
		return tx, true
	}
	return nil, false
}

// db 取当前可用连接：优先事务句柄，其次租户连接。
func db(ctx context.Context) *gorm.DB {
	if tx, ok := txFrom(ctx); ok {
		return tx.WithContext(ctx)
	}
	return tenantdb.DB(ctx)
}
