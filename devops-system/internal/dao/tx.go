// 事务封装：*gorm.DB 只在 dao 出现，service 经本包的聚合事务函数编排，
// 不感知事务句柄（分层约定见 internal/service 包注释）。
package dao

import (
	"context"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-system/internal/tenantdb"
)

// WithTx 在租户感知连接上开启事务（dao 内部多步写共用）。
func WithTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return tenantdb.DB(ctx).Transaction(fn)
}
