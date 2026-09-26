// 租户管理接口（huma ，平台级路由用 kit.plat）。
package api

import (
	"context"

	"devops-infra/errs"
	"devops-infra/http/humaparam"
	"devops-infra/http/response"

	"devops-system/internal/middleware"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// TenantHandler 租户管理接口。
type TenantHandler struct {
	svc *service.Tenant
}

// NewTenantHandler 创建 handler。
func NewTenantHandler(svc *service.Tenant) *TenantHandler { return &TenantHandler{svc: svc} }

// ---- Input / Output ----

// TenantListInput GET /api/v1/tenants 查询参数。
type TenantListInput struct {
	PageNum  int                      `query:"page_num"`
	PageSize int                      `query:"page_size" maximum:"100"`
	Name     string                   `query:"name" maxLength:"30"`
	Code     string                   `query:"code" maxLength:"30"`
	Status   humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1"`
}

// TenantListOutput 租户分页列表信封。
type TenantListOutput struct {
	Body response.BaseResponse[response.PageData[[]types.TenantDTO]]
}

// TenantIDInput path 参数。
type TenantIDInput struct {
	ID types.ID `path:"id"`
}

// TenantGetOutput 租户详情信封。
type TenantGetOutput struct {
	Body response.BaseResponse[types.TenantDTO]
}

// TenantCreateInput POST /api/v1/tenants（含租户初始超管，创建后即可登录）。
type TenantCreateInput struct {
	Body types.TenantCreateReq
}

// TenantCreateOutput 创建响应信封（返回新建租户）。
type TenantCreateOutput struct {
	Body response.BaseResponse[types.TenantDTO]
}

// TenantEmptyOutput 无数据信封。
type TenantEmptyOutput struct {
	Body response.BaseResponse[any]
}

// TenantUpdateInput PUT /api/v1/tenants/{id}（名称/联系人/过期时间）。
type TenantUpdateInput struct {
	ID   types.ID `path:"id"`
	Body types.TenantUpdateReq
}

// TenantStatusInput PUT /api/v1/tenants/{id}/status（停用即全员下线）。
type TenantStatusInput struct {
	ID   types.ID `path:"id"`
	Body types.TenantStatusReq
}

// ---- Handlers ----

// List GET /api/v1/tenants。
func (h *TenantHandler) List(ctx context.Context, in *TenantListInput) (*TenantListOutput, error) {
	list, total, err := h.svc.List(ctx, types.TenantPageQuery{
		PageNum:  in.PageNum,
		PageSize: in.PageSize,
		Name:     in.Name,
		Code:     in.Code,
		Status:   in.Status.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantListOutput{Body: response.PageBody(list, total)}, nil
}

// Get GET /api/v1/tenants/{id}。
func (h *TenantHandler) Get(ctx context.Context, in *TenantIDInput) (*TenantGetOutput, error) {
	resp, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /api/v1/tenants（含租户初始超管；模式B 自动建库+迁移+菜单同步）。
func (h *TenantHandler) Create(ctx context.Context, in *TenantCreateInput) (*TenantCreateOutput, error) {
	resp, err := h.svc.Create(ctx, middleware.ClaimsFrom(ctx), in.Body)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantCreateOutput{Body: response.OKBody(*resp)}, nil
}

// Update PUT /api/v1/tenants/{id}（名称/联系人/过期时间）。
func (h *TenantHandler) Update(ctx context.Context, in *TenantUpdateInput) (*TenantEmptyOutput, error) {
	if err := h.svc.Update(ctx, middleware.ClaimsFrom(ctx), in.ID, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantEmptyOutput{Body: response.EmptyBody()}, nil
}

// UpdateStatus PUT /api/v1/tenants/{id}/status（停用即全员下线）。
func (h *TenantHandler) UpdateStatus(ctx context.Context, in *TenantStatusInput) (*TenantEmptyOutput, error) {
	if err := h.svc.UpdateStatus(ctx, middleware.ClaimsFrom(ctx), in.ID, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /api/v1/tenants/{id}（默认租户不可删；需先停用）。
func (h *TenantHandler) Delete(ctx context.Context, in *TenantIDInput) (*TenantEmptyOutput, error) {
	if err := h.svc.Delete(ctx, middleware.ClaimsFrom(ctx), in.ID); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &TenantEmptyOutput{Body: response.EmptyBody()}, nil
}
