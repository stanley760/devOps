// 租户管理接口（P7，平台级）。
package api

import (
	"github.com/gin-gonic/gin"

	"devops-infra/http/response"

	"devops-system/internal/middleware"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// TenantHandler 租户管理接口。
type TenantHandler struct {
	svc *service.Tenant
}

// NewTenantHandler 创建 handler。
func NewTenantHandler(svc *service.Tenant) *TenantHandler { return &TenantHandler{svc: svc} }

// List GET /api/v1/tenants
func (h *TenantHandler) List(c *gin.Context) error {
	var q types.TenantPageQuery
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

// Get GET /api/v1/tenants/:id
func (h *TenantHandler) Get(c *gin.Context) error {
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

// Create POST /api/v1/tenants（含租户初始超管；模式B 自动建库+迁移+菜单同步）
func (h *TenantHandler) Create(c *gin.Context) error {
	var req types.TenantCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	resp, err := h.svc.Create(c.Request.Context(), middleware.CurrentClaims(c), req)
	if err != nil {
		return err
	}
	response.OK(c, resp)
	return nil
}

// Update PUT /api/v1/tenants/:id（名称/联系人/过期时间）
func (h *TenantHandler) Update(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.TenantUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.Update(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// UpdateStatus PUT /api/v1/tenants/:id/status（停用即全员下线）
func (h *TenantHandler) UpdateStatus(c *gin.Context) error {
	id, err := types.ParseID(c.Param("id"))
	if err != nil {
		return err
	}
	var req types.TenantStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), middleware.CurrentClaims(c), id, req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// Delete DELETE /api/v1/tenants/:id（默认租户不可删；需先停用）
func (h *TenantHandler) Delete(c *gin.Context) error {
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
