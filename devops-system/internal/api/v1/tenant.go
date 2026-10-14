// 租户管理接口（huma ，平台级路由用 kit.plat）。
package api

import (
	"context"

	"github.com/stanley760/devops/devops-common/domain"
	commonResp "github.com/stanley760/devops/devops-common/response"
	"github.com/stanley760/devops/devops-infra/errs"
	"github.com/stanley760/devops/devops-infra/http/humaparam"
	"github.com/stanley760/devops/devops-infra/http/response"

	"github.com/stanley760/devops/devops-system/internal/auth"
	"github.com/stanley760/devops/devops-system/internal/service"
	"github.com/stanley760/devops/devops-system/internal/types"
)

// TenantHandler 租户管理接口。
type TenantHandler struct {
	svc *service.Tenant
}

// NewTenantHandler 创建 handler。
func NewTenantHandler(svc *service.Tenant) *TenantHandler { return &TenantHandler{svc: svc} }

// ---- Input / Output ----

// TenantListInput GET /system/api/v1/tenants 查询参数。
type TenantListInput struct {
	domain.PageParam
	Name   string                   `query:"name" maxLength:"30" doc:"租户名称"`
	Code   string                   `query:"code" maxLength:"30" doc:"租户编码"`
	Status humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1" doc:"租户状态 0:停用；1:启用"`
}

// TenantListOutput 租户分页列表信封。
type TenantListOutput struct {
	Body commonResp.BaseResponse[domain.PageResult[types.TenantDTO]]
}

// TenantIDInput path 参数。
type TenantIDInput struct {
	ID types.ParamID `path:"id" doc:"租户id"`
}

// TenantGetOutput 租户详情信封。
type TenantGetOutput struct {
	Body commonResp.BaseResponse[types.TenantDTO]
}

// TenantCreateInput POST /system/api/v1/tenants（含租户初始超管，创建后即可登录）。
type TenantCreateInput struct {
	Body types.TenantCreateReq
}

// TenantCreateOutput 创建响应信封（返回新建租户）。
type TenantCreateOutput struct {
	Body commonResp.BaseResponse[types.TenantDTO]
}

// TenantEmptyOutput 无数据信封。
type TenantEmptyOutput struct {
	Body commonResp.BaseResponse[any]
}

// TenantUpdateInput PUT /system/api/v1/tenants/{id}（名称/联系人/过期时间）。
type TenantUpdateInput struct {
	ID   types.ParamID `path:"id" doc:"租户id"`
	Body types.TenantUpdateReq
}

// TenantStatusInput PUT /system/api/v1/tenants/{id}/status（停用即全员下线）。
type TenantStatusInput struct {
	ID   types.ParamID `path:"id" doc:"租户id"`
	Body types.TenantStatusReq
}

// ---- Handlers ----

// List GET /system/api/v1/tenants。
func (h *TenantHandler) List(ctx context.Context, in *TenantListInput) (*TenantListOutput, error) {
	page, err := h.svc.List(ctx, types.TenantPageQuery{
		PageParam: in.PageParam,
		Name:      in.Name,
		Code:      in.Code,
		Status:    in.Status.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantListOutput{Body: response.OKBody(page)}, nil
}

// Get GET /system/api/v1/tenants/{id}。
func (h *TenantHandler) Get(ctx context.Context, in *TenantIDInput) (*TenantGetOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	resp, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /system/api/v1/tenants（含租户初始超管）。
func (h *TenantHandler) Create(ctx context.Context, in *TenantCreateInput) (*TenantCreateOutput, error) {
	resp, err := h.svc.Create(ctx, auth.ClaimsFrom(ctx), in.Body)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantCreateOutput{Body: response.OKBody(*resp)}, nil
}

// Update PUT /system/api/v1/tenants/{id}（名称/联系人/过期时间）。
func (h *TenantHandler) Update(ctx context.Context, in *TenantUpdateInput) (*TenantEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Update(ctx, auth.ClaimsFrom(ctx), id, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantEmptyOutput{Body: response.EmptyBody()}, nil
}

// UpdateStatus PUT /system/api/v1/tenants/{id}/status（停用即全员下线）。
func (h *TenantHandler) UpdateStatus(ctx context.Context, in *TenantStatusInput) (*TenantEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.UpdateStatus(ctx, auth.ClaimsFrom(ctx), id, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /system/api/v1/tenants/{id}（默认租户不可删；需先停用）。
func (h *TenantHandler) Delete(ctx context.Context, in *TenantIDInput) (*TenantEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Delete(ctx, auth.ClaimsFrom(ctx), id); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantEmptyOutput{Body: response.EmptyBody()}, nil
}
