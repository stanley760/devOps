// 登录审计管理服务：查询侧（写入侧见 service.Auth.login 的 defer 落库）。
// 删除/清空为运维留存量管控入口，独立权限门控（system:loginlog:delete / :clean）。
package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/stanley760/devOps/devops-common/domain"
	"github.com/stanley760/devOps/devops-infra/errs"
	"github.com/stanley760/devOps/devops-system/internal/dao"
	"github.com/stanley760/devOps/devops-system/internal/model"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// LoginLog 登录审计服务。
type LoginLog struct{}

// NewLoginLog 创建服务（无状态，仅读 dao）。
func NewLoginLog() *LoginLog { return &LoginLog{} }

// List 分页查询登录审计。
func (s *LoginLog) List(ctx context.Context, q types.LoginLogPageQuery) (domain.PageResult[types.LoginLogDTO], error) {
	page, err := dao.ListLoginLogs(ctx, q)
	if err != nil {
		return domain.PageResult[types.LoginLogDTO]{}, err
	}
	return domain.MapPage(page, func(log model.LoginLog) types.LoginLogDTO {
		return types.LoginLogDTO{
			ID:          types.ID(log.ID),
			Username:    log.Username,
			UserID:      types.ID(log.UserID),
			IP:          log.IP,
			UserAgent:   log.UserAgent,
			GeoCountry:  log.GeoCountry,
			GeoProvince: log.GeoProvince,
			GeoCity:     log.GeoCity,
			GeoIsp:      log.GeoIsp,
			Result:      log.Result,
			Code:        log.Code,
			CreateTime:  log.CreateTime,
		}
	}), nil
}

// Delete 批量删除审计行（ids 逗号分隔，如 "12,34,56"）。
func (s *LoginLog) Delete(ctx context.Context, idsCSV string) error {
	ids, err := parseIDsCSV(idsCSV)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errs.New(errs.CodeParam, "ids 不能为空")
	}
	_, err = dao.DeleteLoginLogs(ctx, ids)
	return err
}

// Clean 清空本租户全部审计行。
func (s *LoginLog) Clean(ctx context.Context) error {
	_, err := dao.CleanLoginLogs(ctx)
	return err
}

// parseIDsCSV 解析逗号分隔的 ID 串（容忍空白；任一非法即整体拒绝，不做静默跳过——
// 审计删除必须精确对账）。
func parseIDsCSV(s string) ([]int64, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || v <= 0 {
			return nil, errs.New(errs.CodeParam, "ids 非法：须为正整数逗号分隔（如 12,34,56）")
		}
		ids = append(ids, v)
	}
	return ids, nil
}
