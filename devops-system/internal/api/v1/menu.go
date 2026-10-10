// 菜单管理接口（huma）。
package api

import (
	"context"

	"devops-infra/errs"
	"devops-infra/http/humaparam"
	"devops-infra/http/response"

	"devops-system/internal/auth"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// MenuHandler 菜单管理接口。
type MenuHandler struct {
	svc *service.Menu
}

// NewMenuHandler 创建 handler。
func NewMenuHandler(svc *service.Menu) *MenuHandler { return &MenuHandler{svc: svc} }

// ---- Input / Output ----

// MenuListInput GET /system/api/v1/menus 查询参数（返回树；status 可选）。
type MenuListInput struct {
	Status humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1"`
}

// MenuListOutput 菜单树信封。
type MenuListOutput struct {
	Body response.BaseResponse[[]types.MenuTreeResp]
}

// MenuIDInput path 参数。
type MenuIDInput struct {
	ID types.ID `path:"id"`
}

// MenuGetOutput 菜单详情信封。
type MenuGetOutput struct {
	Body response.BaseResponse[types.MenuDTO]
}

// MenuCreateInput POST /system/api/v1/menus。
type MenuCreateInput struct {
	Body types.MenuSaveReq
}

// MenuEmptyOutput 无数据信封。
type MenuEmptyOutput struct {
	Body response.BaseResponse[any]
}

// MenuUpdateInput PUT /system/api/v1/menus/{id}（permission 变更联动重建引用角色 p 行）。
type MenuUpdateInput struct {
	ID   types.ID `path:"id"`
	Body types.MenuSaveReq
}

// ---- Handlers ----

// List GET /system/api/v1/menus?status=（返回树）。
func (h *MenuHandler) List(ctx context.Context, in *MenuListInput) (*MenuListOutput, error) {
	tree, err := h.svc.List(ctx, in.Status.Ptr())
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &MenuListOutput{Body: response.OKBody(tree)}, nil
}

// Get GET /system/api/v1/menus/{id}。
func (h *MenuHandler) Get(ctx context.Context, in *MenuIDInput) (*MenuGetOutput, error) {
	resp, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &MenuGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /system/api/v1/menus。
func (h *MenuHandler) Create(ctx context.Context, in *MenuCreateInput) (*MenuEmptyOutput, error) {
	if err := h.svc.Create(ctx, auth.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &MenuEmptyOutput{Body: response.EmptyBody()}, nil
}

// Update PUT /system/api/v1/menus/{id}（permission 变更联动重建引用角色 p 行）。
func (h *MenuHandler) Update(ctx context.Context, in *MenuUpdateInput) (*MenuEmptyOutput, error) {
	if err := h.svc.Update(ctx, auth.ClaimsFrom(ctx), in.ID, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &MenuEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /system/api/v1/menus/{id}。
func (h *MenuHandler) Delete(ctx context.Context, in *MenuIDInput) (*MenuEmptyOutput, error) {
	if err := h.svc.Delete(ctx, in.ID); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &MenuEmptyOutput{Body: response.EmptyBody()}, nil
}
