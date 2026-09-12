// 菜单管理接口（P6）。
package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"devops-infra/errs"
	"devops-infra/response"

	"devops-system/internal/middleware"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// MenuHandler 菜单管理接口。
type MenuHandler struct {
	svc *service.Menu
}

// NewMenuHandler 创建 handler。
func NewMenuHandler(svc *service.Menu) *MenuHandler { return &MenuHandler{svc: svc} }

// List GET /api/v1/menus?status=（返回树）
func (h *MenuHandler) List(c *gin.Context) error {
	var status *int8
	if s := c.Query("status"); s != "" {
		v, err := strconv.ParseInt(s, 10, 8)
		if err != nil {
			return errs.New(errs.CodeParam, "status 参数非法")
		}
		sv := int8(v)
		status = &sv
	}
	tree, err := h.svc.List(c.Request.Context(), status)
	if err != nil {
		return err
	}
	response.OK(c, tree)
	return nil
}

// Get GET /api/v1/menus/:id
func (h *MenuHandler) Get(c *gin.Context) error {
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

// Create POST /api/v1/menus
func (h *MenuHandler) Create(c *gin.Context) error {
	var req types.MenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Create(c.Request.Context(), middleware.CurrentClaims(c), req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Update PUT /api/v1/menus/:id（permission 变更联动重建引用角色 p 行）
func (h *MenuHandler) Update(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.MenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Update(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Delete DELETE /api/v1/menus/:id
func (h *MenuHandler) Delete(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}
