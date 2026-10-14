// 操作审计读取侧（写入侧由 humakit 审计采集中间件经 audit.Sink 落库）。
package dao

import (
	"context"

	"github.com/stanley760/devops/devops-common/domain"
	"github.com/stanley760/devops/devops-common/domain/gormx"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-system/internal/model"
	"github.com/stanley760/devops/devops-system/internal/tenantdb"
	"github.com/stanley760/devops/devops-system/internal/types"
)

// ListOperationLogs 分页查询操作审计（新行在前）。
func ListOperationLogs(ctx context.Context, q types.OperationLogPageQuery) (domain.PageResult[model.OperationLog], error) {
	db := tenantdb.DB(ctx).Model(&model.OperationLog{})
	if q.Module != "" {
		db = db.Where("module = ?", q.Module)
	}
	if q.SubModule != "" {
		db = db.Where("sub_module = ?", q.SubModule)
	}
	if q.UserName != "" {
		db = db.Where("user_name LIKE ?", "%"+q.UserName+"%")
	}
	if q.UserIP != "" {
		db = db.Where("user_ip LIKE ?", "%"+q.UserIP+"%")
	}
	if q.Result != nil {
		db = db.Where("result = ?", *q.Result)
	}
	if q.BeginTime != nil {
		db = db.Where("create_time >= ?", *q.BeginTime)
	}
	if q.EndTime != nil {
		db = db.Where("create_time <= ?", *q.EndTime)
	}

	page, err := gormx.FindPage[model.OperationLog](
		db.Order("id DESC"),
		domain.PageParam{PageNum: q.PageNum, PageSize: q.PageSize},
	)
	if err != nil {
		return domain.PageResult[model.OperationLog]{}, errs.Wrap(err, errs.CodeInternal, "查询操作审计列表失败")
	}
	return page, nil
}

// DeleteOperationLogs 批量物理删除审计行。
//
// 物理删除：审计行无软删语义，留下 deleted_at 行只会让表继续膨胀。
// 返回实际删除行数。
func DeleteOperationLogs(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := tenantdb.DB(ctx).Unscoped().
		Where("id IN ?", ids).Delete(&model.OperationLog{})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error, errs.CodeInternal, "删除操作审计失败")
	}
	return res.RowsAffected, nil
}

// CleanOperationLogs 清空本租户全部审计行，返回删除行数。
//
// Unscoped 之后 TenantPlugin 不再注入租户条件，故必须显式带 tenant_id——
// 否则会跨租户清空。
func CleanOperationLogs(ctx context.Context) (int64, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return 0, err
	}
	res := tenantdb.DB(ctx).Unscoped().
		Where("tenant_id = ?", tid).Delete(&model.OperationLog{})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error, errs.CodeInternal, "清空操作审计失败")
	}
	return res.RowsAffected, nil
}
