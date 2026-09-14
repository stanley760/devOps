// 角色管理接口（P6）。
package api

import (
	"github.com/gin-gonic/gin"

	"devops-infra/http/response"

	"devops-system/internal/middleware"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// RoleHandler 角色管理接口。
type RoleHandler struct {
	svc *service.Role
}

// NewRoleHandler 创建 handler。
func NewRoleHandler(svc *service.Role) *RoleHandler { return &RoleHandler{svc: svc} }

// List GET /api/v1/roles
func (h *RoleHandler) List(c *gin.Context) error {
	var q types.RolePageQuery
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

// Get GET /api/v1/roles/:id
func (h *RoleHandler) Get(c *gin.Context) error {
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

// Create POST /api/v1/roles
func (h *RoleHandler) Create(c *gin.Context) error {
	var req types.RoleSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Create(c.Request.Context(), middleware.CurrentClaims(c), req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Update PUT /api/v1/roles/:id
func (h *RoleHandler) Update(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.RoleSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Update(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// AssignMenus PUT /api/v1/roles/:id/menus（role_menu 重建 + p 行快照重建）
func (h *RoleHandler) AssignMenus(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.RoleAssignMenusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.AssignMenus(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Delete DELETE /api/v1/roles/:id
func (h *RoleHandler) Delete(c *gin.Context) error {
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
