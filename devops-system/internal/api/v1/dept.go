// 部门管理接口（P6）。
package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"devops-infra/errs"
	"devops-infra/http/response"

	"devops-system/internal/middleware"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// DeptHandler 部门管理接口。
type DeptHandler struct {
	svc *service.Dept
}

// NewDeptHandler 创建 handler。
func NewDeptHandler(svc *service.Dept) *DeptHandler { return &DeptHandler{svc: svc} }

// List GET /api/v1/depts?status=（返回树）
func (h *DeptHandler) List(c *gin.Context) error {
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

// Get GET /api/v1/depts/:id
func (h *DeptHandler) Get(c *gin.Context) error {
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

// Create POST /api/v1/depts
func (h *DeptHandler) Create(c *gin.Context) error {
	var req types.DeptSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Create(c.Request.Context(), middleware.CurrentClaims(c), req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Update PUT /api/v1/depts/:id
func (h *DeptHandler) Update(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.DeptSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Update(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Delete DELETE /api/v1/depts/:id
func (h *DeptHandler) Delete(c *gin.Context) error {
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
