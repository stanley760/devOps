// 登录审计管理接口（huma）。查询侧；写入侧随登录自动落库，无对应接口。
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

// LoginLogHandler 登录审计接口。
type LoginLogHandler struct {
	svc *service.LoginLog
}

// NewLoginLogHandler 创建 handler。
func NewLoginLogHandler(svc *service.LoginLog) *LoginLogHandler { return &LoginLogHandler{svc: svc} }

// ---- Input / Output ----

// LoginLogListInput GET /system/api/v1/login-logs 查询参数（分页零值走 dao 兜底；
// 可选参数走 humaparam.Optional，huma 管线不支持裸指针 query 参数）。
type LoginLogListInput struct {
	domain.PageParam
	Username  string                        `query:"username" maxLength:"30" doc:"用户名，模糊匹配"`
	IP        string                        `query:"ip" maxLength:"50" doc:"客户端 IP，模糊匹配（支持网段前缀，如 \"192.168.\"）"`
	Result    humaparam.Optional[int8]      `query:"result" minimum:"0" maximum:"1" doc:"登录结果 0:成功；1:失败，不传为不过滤"`
	BeginTime humaparam.Optional[time.Time] `query:"begin_time" doc:"开始时间（含），RFC3339 格式"` // RFC3339（OpenAPI 标准格式）
	EndTime   humaparam.Optional[time.Time] `query:"end_time" doc:"结束时间（含），RFC3339 格式"`
}

// LoginLogListOutput 登录审计分页列表信封。
type LoginLogListOutput struct {
	Body commonResp.BaseResponse[domain.PageResult[types.LoginLogDTO]]
}

// LoginLogDeleteInput DELETE /system/api/v1/login-logs（ids 逗号分隔，如 ?ids=12,34,56）。
type LoginLogDeleteInput struct {
	IDs string `query:"ids" maxLength:"2000" doc:"待删除的记录 ID 列表，逗号分隔，如 12,34,56"`
}

// LoginLogEmptyOutput 无数据信封（删除/清空）。
type LoginLogEmptyOutput struct {
	Body commonResp.BaseResponse[any]
}

// ---- Handlers ----

// List GET /system/api/v1/login-logs。
func (h *LoginLogHandler) List(ctx context.Context, in *LoginLogListInput) (*LoginLogListOutput, error) {
	page, err := h.svc.List(ctx, types.LoginLogPageQuery{
		PageParam: in.PageParam,
		Username:  in.Username,
		IP:        in.IP,
		Result:    in.Result.Ptr(),
		BeginTime: in.BeginTime.Ptr(),
		EndTime:   in.EndTime.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &LoginLogListOutput{Body: response.OKBody(page)}, nil
}

// Delete DELETE /system/api/v1/login-logs（批量删除）。
func (h *LoginLogHandler) Delete(ctx context.Context, in *LoginLogDeleteInput) (*LoginLogEmptyOutput, error) {
	if err := h.svc.Delete(ctx, in.IDs); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &LoginLogEmptyOutput{Body: response.EmptyBody()}, nil
}

// Clean DELETE /system/api/v1/login-logs/clean（清空本租户）。
func (h *LoginLogHandler) Clean(ctx context.Context, _ *struct{}) (*LoginLogEmptyOutput, error) {
	if err := h.svc.Clean(ctx); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &LoginLogEmptyOutput{Body: response.EmptyBody()}, nil
}
