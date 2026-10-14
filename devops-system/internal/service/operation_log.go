// 操作审计管理服务：查询侧（写入侧由 humakit 审计采集中间件自动落库）。
// 删除/清空为运维留存量管控入口，独立权限门控
// （system:operationlog:delete / :clean）。
package service

import (
	"context"

	"github.com/stanley760/devOps/devops-common/domain"
	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/dao"
	"github.com/stanley760/devOps/devops-system/internal/model"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// OperationLog 操作审计服务。
type OperationLog struct{}

// NewOperationLog 创建服务（无状态，仅读 dao）。
func NewOperationLog() *OperationLog { return &OperationLog{} }

// List 分页查询操作审计。
func (s *OperationLog) List(ctx context.Context, q types.OperationLogPageQuery) (domain.PageResult[types.OperationLogDTO], error) {
	page, err := dao.ListOperationLogs(ctx, q)
	if err != nil {
		return domain.PageResult[types.OperationLogDTO]{}, err
	}
	return domain.MapPage(page, func(log model.OperationLog) types.OperationLogDTO {
		return types.OperationLogDTO{
			ID:            types.ID(log.ID),
			TraceID:       log.TraceID,
			UserID:        types.ID(log.UserID),
			UserName:      log.UserName,
			UserType:      log.UserType,
			UserTypeName:  log.UserType.String(),
			Module:        log.Module,
			SubModule:     log.SubModule,
			BizID:         types.ID(log.BizID),
			Contents:      log.Contents,
			Extra:         log.Extra,
			RequestMethod: log.RequestMethod,
			RequestURL:    log.RequestURL,
			UserIP:        log.UserIP,
			UserAgent:     log.UserAgent,
			GeoCountry:    log.GeoCountry,
			GeoProvince:   log.GeoProvince,
			GeoCity:       log.GeoCity,
			GeoIsp:        log.GeoISP,
			Result:        log.Result,
			Code:          log.Code,
			CreateTime:    log.CreateTime,
		}
	}), nil
}

// Delete 批量删除审计行（ids 逗号分隔，如 "12,34,56"）。
func (s *OperationLog) Delete(ctx context.Context, idsCSV string) error {
	ids, err := parseIDsCSV(idsCSV)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errs.New(errs.CodeParam, "ids 不能为空")
	}
	_, err = dao.DeleteOperationLogs(ctx, ids)
	return err
}

// Clean 清空本租户全部审计行。
func (s *OperationLog) Clean(ctx context.Context) error {
	_, err := dao.CleanOperationLogs(ctx)
	return err
}