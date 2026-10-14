// 用户管理接口（huma）。
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

// UserHandler 用户管理接口。
type UserHandler struct {
	svc *service.User
}

// NewUserHandler 创建 handler。
func NewUserHandler(svc *service.User) *UserHandler { return &UserHandler{svc: svc} }

// ---- Input / Output ----

// UserListInput GET /system/api/v1/users 查询参数。
type UserListInput struct {
	domain.PageParam
	Username string                            `query:"username" maxLength:"30" doc:"用户姓名"`
	Mobile   string                            `query:"mobile" maxLength:"20" doc:"手机号码"`
	Status   humaparam.Optional[int8]          `query:"status" minimum:"0" maximum:"1" doc:"账号状态 0:禁用；1:启用"`
	DeptID   humaparam.Optional[types.ParamID] `query:"dept_id" doc:"关联部门id"`
}

// UserListOutput 用户分页列表信封。
type UserListOutput struct {
	Body commonResp.BaseResponse[domain.PageResult[types.UserDTO]]
}

// UserIDInput path 参数。
type UserIDInput struct {
	ID types.ParamID `path:"id" doc:"用户id"`
}

// UserGetOutput 用户详情信封（基本信息 + 角色/岗位 ID）。
type UserGetOutput struct {
	Body commonResp.BaseResponse[types.UserDetailResp]
}

// UserCreateInput POST /system/api/v1/users（角色分配 g 行联动）。
type UserCreateInput struct {
	Body types.UserCreateReq
}

// UserEmptyOutput 无数据信封。
type UserEmptyOutput struct {
	Body commonResp.BaseResponse[any]
}

// UserUpdateInput PUT /system/api/v1/users/{id}（密码走重置接口）。
type UserUpdateInput struct {
	ID   types.ParamID `path:"id" doc:"用户id"`
	Body types.UserUpdateReq
}

// UserResetPasswordInput PUT /system/api/v1/users/{id}/password（重置后踢下线）。
type UserResetPasswordInput struct {
	ID   types.ParamID `path:"id" doc:"用户id"`
	Body types.ResetPasswordReq
}

// ---- Handlers ----

// List GET /system/api/v1/users。
func (h *UserHandler) List(ctx context.Context, in *UserListInput) (*UserListOutput, error) {
	var deptID *types.ID
	if in.DeptID.IsSet {
		id, err := in.DeptID.Value.Parse()
		if err != nil {
			return nil, errs.ToHumaError(err)
		}
		deptID = &id
	}
	page, err := h.svc.List(ctx, types.UserPageQuery{
		PageParam: in.PageParam,
		Username:  in.Username,
		Mobile:    in.Mobile,
		Status:    in.Status.Ptr(),
		DeptID:    deptID,
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserListOutput{Body: response.OKBody(page)}, nil
}

// Get GET /system/api/v1/users/{id}。
func (h *UserHandler) Get(ctx context.Context, in *UserIDInput) (*UserGetOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	resp, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /system/api/v1/users。
func (h *UserHandler) Create(ctx context.Context, in *UserCreateInput) (*UserEmptyOutput, error) {
	if err := h.svc.Create(ctx, auth.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}

// Update PUT /system/api/v1/users/{id}。
func (h *UserHandler) Update(ctx context.Context, in *UserUpdateInput) (*UserEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Update(ctx, auth.ClaimsFrom(ctx), id, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}

// ResetPassword PUT /system/api/v1/users/{id}/password（重置后踢下线）。
func (h *UserHandler) ResetPassword(ctx context.Context, in *UserResetPasswordInput) (*UserEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.ResetPassword(ctx, auth.ClaimsFrom(ctx), id, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /system/api/v1/users/{id}。
func (h *UserHandler) Delete(ctx context.Context, in *UserIDInput) (*UserEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Delete(ctx, auth.ClaimsFrom(ctx), id); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UserEmptyOutput{Body: response.EmptyBody()}, nil
}
