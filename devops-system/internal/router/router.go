// Package router 注册 devops-system 的业务路由。
//
// /healthz 与统一中间件（Recovery/AccessLog/404/405 信封）由 devops-infra/httpx 提供。
// 中间件链（方案 6.2）：
//
//	公开：/auth/login /auth/refresh
//	登录：JWT → Tenant → 业务（auth/info 等）
//	平台：JWT → Platform → 租户管理（P5/P8 挂载）
package router

import (
	"github.com/gin-gonic/gin"

	"devops-infra/httpx"
	"devops-infra/response"

	apiv1 "devops-system/internal/api/v1"
	"devops-system/internal/auth"
	"devops-system/internal/middleware"
	"devops-system/internal/service"
)

// Register 返回业务路由注册回调，交给 httpx.Run 组装。
func Register(authM *auth.Manager, authSvc *service.Auth) httpx.Register {
	authH := apiv1.NewAuthHandler(authSvc)

	return func(r *gin.Engine) {
		api := r.Group("/api/v1")

		// 公开：登录 / 刷新
		api.POST("/auth/login", wrap(authH.Login))
		api.POST("/auth/refresh", wrap(authH.Refresh))

		// 登录态：登出（仅需 JWT）
		logged := api.Group("", middleware.JWT(authM))
		logged.POST("/auth/logout", wrap(authH.Logout))

		// 登录态 + 租户校验
		biz := logged.Group("", middleware.Tenant())
		biz.GET("/auth/info", wrap(authH.Info))
		biz.GET("/auth/routers", wrap(authH.Routers))
		biz.PUT("/auth/profile", wrap(authH.UpdateProfile))
		biz.PUT("/auth/profile/password", wrap(authH.UpdatePassword))

		// P5+：users/roles/menus/depts/posts（JWT → Tenant → CasbinAuth）
		// P8：tenants（JWT → Platform → CasbinAuth）
	}
}

// wrap 适配 response.Handler → gin.HandlerFunc（统一错误出口）。
func wrap(h response.Handler) gin.HandlerFunc { return response.Wrap(h) }
