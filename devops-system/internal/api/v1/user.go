// 用户管理接口（P5）。
package api

import (
	"github.com/gin-gonic/gin"

	"devops-infra/response"

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

// List GET /api/v1/users
func (h *UserHandler) List(c *gin.Context) error {
	var q types.UserPageQuery
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

// Get GET /api/v1/users/:id
func (h *UserHandler) Get(c *gin.Context) error {
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

// Create POST /api/v1/users
func (h *UserHandler) Create(c *gin.Context) error {
	var req types.UserCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Create(c.Request.Context(), middleware.CurrentClaims(c), req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Update PUT /api/v1/users/:id
func (h *UserHandler) Update(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.UserUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Update(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// ResetPassword PUT /api/v1/users/:id/password（重置后踢下线）
func (h *UserHandler) ResetPassword(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.ResetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.ResetPassword(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Delete DELETE /api/v1/users/:id
func (h *UserHandler) Delete(c *gin.Context) error {
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
