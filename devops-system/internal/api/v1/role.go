// 角色管理接口（huma）。
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

// RoleHandler 角色管理接口。
type RoleHandler struct {
	svc *service.Role
}

// NewRoleHandler 创建 handler。
func NewRoleHandler(svc *service.Role) *RoleHandler { return &RoleHandler{svc: svc} }

// RoleListInput GET /system/api/v1/roles 查询参数。
type RoleListInput struct {
	PageNum  int                      `query:"page_num"`
	PageSize int                      `query:"page_size" maximum:"100"`
	Name     string                   `query:"name" maxLength:"30"`
	Code     string                   `query:"code" maxLength:"100"`
	Status   humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1"`
}

// RoleListOutput 角色分页列表信封。
type RoleListOutput struct {
	Body response.BaseResponse[response.PageData[[]types.RoleDTO]]
}

// RoleIDInput path 参数。
type RoleIDInput struct {
	ID types.ID `path:"id"`
}

// RoleGetOutput 角色详情信封（角色 + 已分配菜单 ID）。
type RoleGetOutput struct {
	Body response.BaseResponse[types.RoleDetailResp]
}

// RoleCreateInput POST /system/api/v1/roles。
type RoleCreateInput struct {
	Body types.RoleSaveReq
}

// RoleEmptyOutput 无数据信封。
type RoleEmptyOutput struct {
	Body response.BaseResponse[any]
}

// RoleUpdateInput PUT /system/api/v1/roles/{id}。
type RoleUpdateInput struct {
	ID   types.ID `path:"id"`
	Body types.RoleSaveReq
}

// RoleAssignMenusInput PUT /system/api/v1/roles/{id}/menus（重建 role_menu + p 行）。
type RoleAssignMenusInput struct {
	ID   types.ID `path:"id"`
	Body types.RoleAssignMenusReq
}

// List GET /system/api/v1/roles。
func (h *RoleHandler) List(ctx context.Context, in *RoleListInput) (*RoleListOutput, error) {
	list, total, err := h.svc.List(ctx, types.RolePageQuery{
		PageNum:  in.PageNum,
		PageSize: in.PageSize,
		Name:     in.Name,
		Code:     in.Code,
		Status:   in.Status.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RoleListOutput{Body: response.PageBody(list, total)}, nil
}

// Get GET /system/api/v1/roles/{id}。
func (h *RoleHandler) Get(ctx context.Context, in *RoleIDInput) (*RoleGetOutput, error) {
	resp, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RoleGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /system/api/v1/roles。
func (h *RoleHandler) Create(ctx context.Context, in *RoleCreateInput) (*RoleEmptyOutput, error) {
	if err := h.svc.Create(ctx, auth.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RoleEmptyOutput{Body: response.EmptyBody()}, nil
}

// Update PUT /system/api/v1/roles/{id}。
func (h *RoleHandler) Update(ctx context.Context, in *RoleUpdateInput) (*RoleEmptyOutput, error) {
	if err := h.svc.Update(ctx, auth.ClaimsFrom(ctx), in.ID, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RoleEmptyOutput{Body: response.EmptyBody()}, nil
}

// AssignMenus PUT /system/api/v1/roles/{id}/menus（role_menu 重建 + p 行快照重建）。
func (h *RoleHandler) AssignMenus(ctx context.Context, in *RoleAssignMenusInput) (*RoleEmptyOutput, error) {
	if err := h.svc.AssignMenus(ctx, auth.ClaimsFrom(ctx), in.ID, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RoleEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /system/api/v1/roles/{id}。
func (h *RoleHandler) Delete(ctx context.Context, in *RoleIDInput) (*RoleEmptyOutput, error) {
	if err := h.svc.Delete(ctx, auth.ClaimsFrom(ctx), in.ID); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RoleEmptyOutput{Body: response.EmptyBody()}, nil
}
