// Package api HTTP handler 层（薄适配：绑定 → 调 service → 统一响应）。
package api

import (
	"github.com/gin-gonic/gin"

	"devops-infra/response"

	"devops-system/internal/middleware"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// AuthHandler 认证接口。
type AuthHandler struct {
	svc *service.Auth
}

// NewAuthHandler 创建 handler。
func NewAuthHandler(svc *service.Auth) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Login POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) error {
	var req types.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	resp, err := h.svc.Login(c.Request.Context(), req, c.ClientIP())
	if err != nil {
		return err
	}
	response.OK(c, resp)
	return nil
}

// Refresh POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) error {
	var req types.RefreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	resp, err := h.svc.Refresh(c.Request.Context(), req)
	if err != nil {
		return err
	}
	response.OK(c, resp)
	return nil
}

// Logout POST /api/v1/auth/logout（需 JWT）
func (h *AuthHandler) Logout(c *gin.Context) error {
	h.svc.Logout(c.Request.Context(), middleware.CurrentClaims(c))
	response.OK(c, nil)
	return nil
}

// Info GET /api/v1/auth/info（需 JWT + Tenant）
func (h *AuthHandler) Info(c *gin.Context) error {
	resp, err := h.svc.Info(c.Request.Context(), middleware.CurrentClaims(c))
	if err != nil {
		return err
	}
	response.OK(c, resp)
	return nil
}

// Routers GET /api/v1/auth/routers（需 JWT + Tenant）
func (h *AuthHandler) Routers(c *gin.Context) error {
	routers, err := h.svc.Routers(c.Request.Context(), middleware.CurrentClaims(c))
	if err != nil {
		return err
	}
	response.OK(c, routers)
	return nil
}

// UpdateProfile PUT /api/v1/auth/profile（需 JWT + Tenant）
func (h *AuthHandler) UpdateProfile(c *gin.Context) error {
	var req types.UpdateProfileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.UpdateProfile(c.Request.Context(), middleware.CurrentClaims(c), req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}

// UpdatePassword PUT /api/v1/auth/profile/password（需 JWT + Tenant，改密踢下线）
func (h *AuthHandler) UpdatePassword(c *gin.Context) error {
	var req types.UpdatePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return err
	}
	if err := h.svc.UpdatePassword(c.Request.Context(), middleware.CurrentClaims(c), req); err != nil {
		return err
	}
	response.OK(c, nil)
	return nil
}
