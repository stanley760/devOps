// 岗位管理接口（P6）。
package api

import (
	"github.com/gin-gonic/gin"

	"devops-infra/response"

	"devops-system/internal/middleware"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// PostHandler 岗位管理接口。
type PostHandler struct {
	svc *service.Post
}

// NewPostHandler 创建 handler。
func NewPostHandler(svc *service.Post) *PostHandler { return &PostHandler{svc: svc} }

// List GET /api/v1/posts
func (h *PostHandler) List(c *gin.Context) error {
	var q types.PostPageQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		return err
	}
	list, total, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		return err
	}
	response.OKPage(c, list, total)
	return nil
}

// Get GET /api/v1/posts/:id
func (h *PostHandler) Get(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	resp, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		return err
	}
	response.OK(c, resp)
	return nil
}

// Create POST /api/v1/posts
func (h *PostHandler) Create(c *gin.Context) error {
	var req types.PostSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Create(c.Request.Context(), middleware.CurrentClaims(c), req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Update PUT /api/v1/posts/:id
func (h *PostHandler) Update(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.PostSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Update(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Delete DELETE /api/v1/posts/:id
func (h *PostHandler) Delete(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.Request.Context(), middleware.CurrentClaims(c), id); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}
