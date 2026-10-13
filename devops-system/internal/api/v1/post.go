// 岗位管理接口（huma）。
package api

import (
	"context"

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
	PageNum  int                      `query:"page_num"`
	PageSize int                      `query:"page_size" maximum:"100"`
	Code     string                   `query:"code" maxLength:"64"`
	Name     string                   `query:"name" maxLength:"50"`
	Status   humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1"`
}

// PostListOutput 岗位分页列表信封。
type PostListOutput struct {
	Body response.BaseResponse[response.PageData[[]types.PostDTO]]
}

// PostIDInput path 参数。
type PostIDInput struct {
	ID types.ParamID `path:"id"`
}

// PostGetOutput 岗位详情信封。
type PostGetOutput struct {
	Body response.BaseResponse[types.PostDTO]
}

// PostCreateInput POST /system/api/v1/posts。
type PostCreateInput struct {
	Body types.PostSaveReq
}

// PostEmptyOutput 无数据信封（创建/更新/删除）。
type PostEmptyOutput struct {
	Body response.BaseResponse[any]
}

// PostUpdateInput PUT /system/api/v1/posts/{id}。
type PostUpdateInput struct {
	ID   types.ParamID `path:"id"`
	Body types.PostSaveReq
}

// ---- Handlers ----

// List GET /system/api/v1/posts。
func (h *PostHandler) List(ctx context.Context, in *PostListInput) (*PostListOutput, error) {
	list, total, err := h.svc.List(ctx, types.PostPageQuery{
		PageNum:  in.PageNum,
		PageSize: in.PageSize,
		Code:     in.Code,
		Name:     in.Name,
		Status:   in.Status.Ptr(),
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &PostListOutput{Body: response.PageBody(list, total)}, nil
}

// Get GET /system/api/v1/posts/{id}。
func (h *PostHandler) Get(ctx context.Context, in *PostIDInput) (*PostGetOutput, error) {
	id, err := in.ID.Parse()
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
	id, err := in.ID.Parse()
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
	id, err := in.ID.Parse()
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	if err := h.svc.Delete(ctx, auth.ClaimsFrom(ctx), id); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &PostEmptyOutput{Body: response.EmptyBody()}, nil
}
