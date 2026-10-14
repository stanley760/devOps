// Package audit 提供 devops-system 的操作审计装配。
//
// 分层：契约（记录什么、列宽多宽、用户类型枚举）在 devops-common/audit；
// 通用 GORM 写入实现在 devops-infra/audit；本包只做本服务的落位接线——
// 把租户感知连接（tenantdb.DB）与业务表模型（model.OperationLog）接到一起。
package audit

import (
	"context"

	"gorm.io/gorm"

	infraaudit "github.com/stanley760/devops/devops-infra/audit"

	"github.com/stanley760/devops/devops-system/internal/model"
	"github.com/stanley760/devops/devops-system/internal/tenantdb"
)

// Executor 按请求上下文解析数据库连接。
//
// 走 tenantdb.DB(ctx) 而非裸连接：既复用 ctx 中已开启的事务（业务写与审计写
// 同事务提交），又让 TenantPlugin 依据 ctx 注入 tenant_id——
// 审计行天然落在正确租户下，无需在转换函数里手工填 TenantID。
type Executor struct{}

// Create 实现 infraaudit.Executor。
func (Executor) Create(ctx context.Context, value any) *gorm.DB {
	return tenantdb.DB(ctx).Create(value)
}

// NewSink 装配本服务的操作审计 Sink。
func NewSink() *infraaudit.GORSink[*model.OperationLog] {
	return infraaudit.NewGORSink(infraaudit.GORSinkConfig[*model.OperationLog]{
		Executor: Executor{},
		Convert:  model.FromRecord,
	})
}
