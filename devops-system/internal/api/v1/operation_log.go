// 操作审计管理接口（huma）。仅查询侧；写入侧由审计采集中间件自动落库。
package api

import (
	"context"
	"time"

	"github.com/stanley760/devOps/devops-common/domain"
	commonResp "github.com/stanley760/devOps/devops-common/response"
	"github.com/stanley760/devOps/devops-infra/errs"
	"github.com/stanley760/devOps/devops-infra/http/humaparam"
	"github.com/stanley760/devOps/devops-infra/http/response"

	"github.com/stanley760/devOps/devops-system/internal/service"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// OperationLogHandler 操作审计接口。
type OperationLogHandler struct {
	svc *service.OperationLog
}

// NewOperationLogHandler 创建 handler。
func NewOperationLogHandler(svc *service.OperationLog) *OperationLogHandler {
	return &OperationLogHandler{svc: svc}
}

// ---- Input / Output ----

// OperationLogListInput GET /system/api/v1/operation-logs 查询参数。
type OperationLogListInput struct {
	domain.PageParam
	Module    string                        `query:"module" maxLength:"50" doc:"一级模块，精确匹配"`
	SubModule string                        `query:"sub_module" maxLength:"50" doc:"二级模块，精确匹配"`
	UserName  string                        `query:"user_name" maxLength:"30" doc:"操作者名称，模糊匹配"`
	UserIP    string                        `query:"user_ip" maxLength:"50" doc:"客户端 IP，模糊匹配"`
	Result    humaparam.Optional[int8]      `query:"result" minimum:"0" maximum:"1" doc:"执行结果 0:成功；1:失败，不传为不过滤"`
	BeginTime humaparam.Optional[time.Time] `query:"begin_time" doc:"开始时间（含），RFC3339 格式"`
	EndTime   humaparam.Optional[time.Time] `query:"end_time" doc:"结束时间（含），RFC3339 格式"`
}

// OperationLogListOutput 操作审计分页列表信封。
type OperationLogListOutput struct {
	Body commonResp.BaseResponse[domain.PageResult[types.OperationLogDTO]]
}

// OperationLogDeleteInput DELETE /system/api/v1/operation-logs（ids 逗号分隔）。
type OperationLogDeleteInput struct {
	IDs string `query:"ids" maxLength:"2000" doc:"待删除的记录 ID 列表，逗号分隔，如 12,34,56"`
}

// OperationLogEmptyOutput 无数据信封（删除/清空）。
type OperationLogEmptyOutput struct {
	Body commonResp.BaseResponse[any]
}

// ---- Handlers ----

// List GET /system/api/v1/operation-logs。
func (h *OperationLogHandler) List(ctx context.Context, in *OperationLogListInput) (*OperationLogListOutput, error) {
	page, err := h.svc.List(ctx, types.OperationLogPageQuery{
		PageParam: in.PageParam,
		Module:    in.Module,
		SubModule: in.SubModule,
		UserName:  in.UserName,
		UserIP:    in.UserIP,
		Result:    in.Result.Ptr(),
		BeginTime: in.BeginTime.Ptr(),
		EndTime:   in.EndTime.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &OperationLogListOutput{Body: response.OKBody(page)}, nil
}

// Delete DELETE /system/api/v1/operation-logs（批量删除）。
func (h *OperationLogHandler) Delete(ctx context.Context, in *OperationLogDeleteInput) (*OperationLogEmptyOutput, error) {
	if err := h.svc.Delete(ctx, in.IDs); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &OperationLogEmptyOutput{Body: response.EmptyBody()}, nil
}

// Clean DELETE /system/api/v1/operation-logs/clean（清空本租户）。
func (h *OperationLogHandler) Clean(ctx context.Context, _ *struct{}) (*OperationLogEmptyOutput, error) {
	if err := h.svc.Clean(ctx); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &OperationLogEmptyOutput{Body: response.EmptyBody()}, nil
}