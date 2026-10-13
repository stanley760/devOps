// 登录审计管理接口（huma）。查询侧；写入侧随登录自动落库，无对应接口。
package api

import (
	"context"
	"time"

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
	PageNum   int                           `query:"page_num"`
	PageSize  int                           `query:"page_size" maximum:"100"`
	Username  string                        `query:"username" maxLength:"30"`
	IP        string                        `query:"ip" maxLength:"50"`
	Result    humaparam.Optional[int8]      `query:"result" minimum:"0" maximum:"1"`
	BeginTime humaparam.Optional[time.Time] `query:"begin_time"` // RFC3339（OpenAPI 标准格式）
	EndTime   humaparam.Optional[time.Time] `query:"end_time"`
}

// LoginLogListOutput 登录审计分页列表信封。
type LoginLogListOutput struct {
	Body response.BaseResponse[response.PageData[[]types.LoginLogDTO]]
}

// LoginLogDeleteInput DELETE /system/api/v1/login-logs（ids 逗号分隔，如 ?ids=12,34,56）。
type LoginLogDeleteInput struct {
	IDs string `query:"ids" maxLength:"2000"`
}

// LoginLogEmptyOutput 无数据信封（删除/清空）。
type LoginLogEmptyOutput struct {
	Body response.BaseResponse[any]
}

// ---- Handlers ----

// List GET /system/api/v1/login-logs。
func (h *LoginLogHandler) List(ctx context.Context, in *LoginLogListInput) (*LoginLogListOutput, error) {
	list, total, err := h.svc.List(ctx, types.LoginLogPageQuery{
		PageNum:   in.PageNum,
		PageSize:  in.PageSize,
		Username:  in.Username,
		IP:        in.IP,
		Result:    in.Result.Ptr(),
		BeginTime: in.BeginTime.Ptr(),
		EndTime:   in.EndTime.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &LoginLogListOutput{Body: response.PageBody(list, total)}, nil
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
