// 用户管理接口（huma）。
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

// UserHandler 用户管理接口。
type UserHandler struct {
	svc *service.User
}

// NewUserHandler 创建 handler。
func NewUserHandler(svc *service.User) *UserHandler { return &UserHandler{svc: svc} }

// ---- Input / Output ----

// UserListInput GET /api/v1/users 查询参数。
type UserListInput struct {
	PageNum  int                          `query:"page_num"`
	PageSize int                          `query:"page_size" maximum:"100"`
	Username string                       `query:"username" maxLength:"30"`
	Mobile   string                       `query:"mobile" maxLength:"20"`
	Status   humaparam.Optional[int8]     `query:"status" minimum:"0" maximum:"1"`
	DeptID   humaparam.Optional[types.ID] `query:"dept_id"`
}

// UserListOutput 用户分页列表信封。
type UserListOutput struct {
	Body response.BaseResponse[response.PageData[[]types.UserDTO]]
}

// UserIDInput path 参数。
type UserIDInput struct {
	ID types.ID `path:"id"`
}

// UserGetOutput 用户详情信封（基本信息 + 角色/岗位 ID）。
type UserGetOutput struct {
	Body response.BaseResponse[types.UserDetailResp]
}

// UserCreateInput POST /api/v1/users（角色分配 g 行联动）。
type UserCreateInput struct {
	Body types.UserCreateReq
}

// UserEmptyOutput 无数据信封。
type UserEmptyOutput struct {
	Body response.BaseResponse[any]
}

// UserUpdateInput PUT /api/v1/users/{id}（密码走重置接口）。
type UserUpdateInput struct {
	ID   types.ID `path:"id"`
	Body types.UserUpdateReq
}

// UserResetPasswordInput PUT /api/v1/users/{id}/password（重置后踢下线）。
type UserResetPasswordInput struct {
	ID   types.ID `path:"id"`
	Body types.ResetPasswordReq
}

// ---- Handlers ----

// List GET /api/v1/users。
func (h *UserHandler) List(ctx context.Context, in *UserListInput) (*UserListOutput, error) {
	list, total, err := h.svc.List(ctx, types.UserPageQuery{
		PageNum:  in.PageNum,
		PageSize: in.PageSize,
		Username: in.Username,
		Mobile:   in.Mobile,
		Status:   in.Status.Ptr(),
		DeptID:   in.DeptID.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserListOutput{Body: response.PageBody(list, total)}, nil
}

// Get GET /api/v1/users/{id}。
func (h *UserHandler) Get(ctx context.Context, in *UserIDInput) (*UserGetOutput, error) {
	resp, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /api/v1/users。
func (h *UserHandler) Create(ctx context.Context, in *UserCreateInput) (*UserEmptyOutput, error) {
	if err := h.svc.Create(ctx, middleware.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}

// Update PUT /api/v1/users/{id}。
func (h *UserHandler) Update(ctx context.Context, in *UserUpdateInput) (*UserEmptyOutput, error) {
	if err := h.svc.Update(ctx, middleware.ClaimsFrom(ctx), in.ID, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}

// ResetPassword PUT /api/v1/users/{id}/password（重置后踢下线）。
func (h *UserHandler) ResetPassword(ctx context.Context, in *UserResetPasswordInput) (*UserEmptyOutput, error) {
	if err := h.svc.ResetPassword(ctx, middleware.ClaimsFrom(ctx), in.ID, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /api/v1/users/{id}。
func (h *UserHandler) Delete(ctx context.Context, in *UserIDInput) (*UserEmptyOutput, error) {
	if err := h.svc.Delete(ctx, middleware.ClaimsFrom(ctx), in.ID); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}
