// 岗位管理接口（huma）。
package api

import (
	"context"

	"github.com/stanley760/devOps/devops-common/domain"
	commonResp "github.com/stanley760/devOps/devops-common/response"
	"github.com/stanley760/devOps/devops-infra/errs"
	"github.com/stanley760/devOps/devops-infra/http/humaparam"
	"github.com/stanley760/devOps/devops-infra/http/response"

	"github.com/stanley760/devOps/devops-system/internal/auth"
	"github.com/stanley760/devOps/devops-system/internal/service"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// PostHandler 岗位管理接口。
type PostHandler struct {
	svc *service.Post
}

// NewPostHandler 创建 handler。
func NewPostHandler(svc *service.Post) *PostHandler { return &PostHandler{svc: svc} }

// ---- Input / Output ----

// PostListInput GET /system/api/v1/posts 查询参数（分页零值走 dao 兜底，分页字段不加 minimum）。
type PostListInput struct {
	domain.PageParam
	Code   string                   `query:"code" maxLength:"64" doc:"岗位编码"`
	Name   string                   `query:"name" maxLength:"50" doc:"岗位名称"`
	Status humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1" doc:"岗位状态 0:禁用；1:启用"`
}

// PostListOutput 岗位分页列表信封。
type PostListOutput struct {
	Body commonResp.BaseResponse[domain.PageResult[types.PostDTO]]
}

// PostIDInput path 参数。
type PostIDInput struct {
	ID types.ParamID `path:"id" doc:"岗位id"`
}

// PostGetOutput 岗位详情信封。
type PostGetOutput struct {
	Body commonResp.BaseResponse[types.PostDTO]
}

// PostCreateInput POST /system/api/v1/posts。
type PostCreateInput struct {
	Body types.PostSaveReq
}

// PostEmptyOutput 无数据信封（创建/更新/删除）。
type PostEmptyOutput struct {
	Body commonResp.BaseResponse[any]
}

// PostUpdateInput PUT /system/api/v1/posts/{id}。
type PostUpdateInput struct {
	ID   types.ParamID `path:"id" doc:"岗位id"`
	Body types.PostSaveReq
}

// ---- Handlers ----

// List GET /system/api/v1/posts。
func (h *PostHandler) List(ctx context.Context, in *PostListInput) (*PostListOutput, error) {
	page, err := h.svc.List(ctx, types.PostPageQuery{
		PageParam: in.PageParam,
		Code:      in.Code,
		Name:      in.Name,
		Status:    in.Status.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &PostListOutput{Body: response.OKBody(page)}, nil
}

// Get GET /system/api/v1/posts/{id}。
func (h *PostHandler) Get(ctx context.Context, in *PostIDInput) (*PostGetOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	resp, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &PostGetOutput{Body: response.OKBody(*resp)}, nil
}

// Create POST /system/api/v1/posts（code 创建后不可改）。
func (h *PostHandler) Create(ctx context.Context, in *PostCreateInput) (*PostEmptyOutput, error) {
	if err := h.svc.Create(ctx, auth.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &PostEmptyOutput{Body: response.EmptyBody()}, nil
}

// Update PUT /system/api/v1/posts/{id}。
func (h *PostHandler) Update(ctx context.Context, in *PostUpdateInput) (*PostEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Update(ctx, auth.ClaimsFrom(ctx), id, in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &PostEmptyOutput{Body: response.EmptyBody()}, nil
}

// Delete DELETE /system/api/v1/posts/{id}。
func (h *PostHandler) Delete(ctx context.Context, in *PostIDInput) (*PostEmptyOutput, error) {
	id, err := types.Parse(in.ID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Delete(ctx, auth.ClaimsFrom(ctx), id); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &PostEmptyOutput{Body: response.EmptyBody()}, nil
}
