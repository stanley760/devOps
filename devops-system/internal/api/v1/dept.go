// 部门管理接口（huma）。
package api

import (
	"context"

	commonResp "github.com/stanley760/devOps/devops-common/response"
	"github.com/stanley760/devOps/devops-infra/errs"
	"github.com/stanley760/devOps/devops-infra/http/humaparam"
	"github.com/stanley760/devOps/devops-infra/http/response"

	"github.com/stanley760/devOps/devops-system/internal/auth"
	"github.com/stanley760/devOps/devops-system/internal/service"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// DeptHandler 部门管理接口。
type DeptHandler struct {
	svc *service.Dept
}

// NewDeptHandler 创建 handler。
func NewDeptHandler(svc *service.Dept) *DeptHandler { return &DeptHandler{svc: svc} }

// ---- Input / Output ----

// DeptListInput GET /system/api/v1/depts 查询参数（返回树；status 可选）。
type DeptListInput struct {
	Status humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1" doc:"部门状态 0:禁用；1:启用"`
}

// DeptListOutput 部门树信封。
type DeptListOutput struct {
	Body commonResp.BaseResponse[[]types.DeptTreeResp]
}

// DeptIDInput path 参数。
type DeptIDInput struct {
	ID types.ParamID `path:"id" doc:"部门id"`
}

// DeptGetOutput 部门详情信封。
type DeptGetOutput struct {
	Body commonResp.BaseResponse[types.DeptDTO]
}

// DeptCreateInput POST /system/api/v1/depts。
type DeptCreateInput struct {
	Body types.DeptSaveReq
}

// DeptEmptyOutput 无数据信封。
type DeptEmptyOutput struct {
	Body commonResp.BaseResponse[any]
}

// DeptUpdateInput PUT /system/api/v1/depts/{id}。
type DeptUpdateInput struct {
	ID   types.ParamID `path:"id" doc:"部门id"`
	Body types.DeptSaveReq
}

// ---- Handlers ----

// List GET /system/api/v1/depts?status=（返回树）。
func (h *DeptHandler) List(ctx context.Context, in *DeptListInput) (*DeptListOutput, error) {
	tree, err := h.svc.List(ctx, in.Status.Ptr())
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &DeptListOutput{Body: response.OKBody(tree)}, nil
}

// Get GET /system/api/v1/depts/{id}。
func (h *DeptHandler) Get(ctx context.Context, in *DeptIDInput) (*DeptGetOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	resp, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &DeptGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /system/api/v1/depts。
func (h *DeptHandler) Create(ctx context.Context, in *DeptCreateInput) (*DeptEmptyOutput, error) {
	if err := h.svc.Create(ctx, auth.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &DeptEmptyOutput{Body: response.EmptyBody()}, nil
}

// Update PUT /system/api/v1/depts/{id}（父节点不得是自身或后代，防环）。
func (h *DeptHandler) Update(ctx context.Context, in *DeptUpdateInput) (*DeptEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Update(ctx, auth.ClaimsFrom(ctx), id, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &DeptEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /system/api/v1/depts/{id}。
func (h *DeptHandler) Delete(ctx context.Context, in *DeptIDInput) (*DeptEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Delete(ctx, id); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &DeptEmptyOutput{Body: response.EmptyBody()}, nil
}
