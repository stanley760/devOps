// Package router 注册 devops-system 的业务路由。
//
// /healthz 与统一中间件（Recovery/AccessLog/404/405 信封）由 devops-infra/httpx 提供。
// 中间件链（方案 6.2）：
//
//	公开：/auth/login /auth/refresh
//	登录：JWT → Tenant → Perm(域内) → 业务（users/roles/menus/depts/posts）
//	平台：JWT → PlatformPerm(g2) → 租户管理（/tenants）
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
func Register(authM *auth.Manager, authSvc *service.Auth, userSvc *service.User,
	roleSvc *service.Role, menuSvc *service.Menu, deptSvc *service.Dept, postSvc *service.Post,
	tenantSvc *service.Tenant,
) httpx.Register {
	authH := apiv1.NewAuthHandler(authSvc)
	userH := apiv1.NewUserHandler(userSvc)
	roleH := apiv1.NewRoleHandler(roleSvc)
	menuH := apiv1.NewMenuHandler(menuSvc)
	deptH := apiv1.NewDeptHandler(deptSvc)
	postH := apiv1.NewPostHandler(postSvc)
	tenantH := apiv1.NewTenantHandler(tenantSvc)

	return func(r *gin.Engine) {
		api := r.Group("/api/v1")

		// 公开：登录 / 刷新
		api.POST("/auth/login", wrap(authH.Login))
		api.POST("/auth/refresh", wrap(authH.Refresh))

		// 登录态：登出（仅需 JWT）
		logged := api.Group("", middleware.JWT(authM))
		logged.POST("/auth/logout", wrap(authH.Logout))

		// 登录态 + 租户校验 + 域内鉴权
		// 注意：gin 按注册顺序执行，Perm 必须在 handler 之前（否则鉴权形同虚设）
		biz := logged.Group("", middleware.Tenant())
		biz.GET("/auth/info", wrap(authH.Info))
		biz.GET("/auth/routers", wrap(authH.Routers))
		biz.PUT("/auth/profile", wrap(authH.UpdateProfile))
		biz.PUT("/auth/profile/password", wrap(authH.UpdatePassword))

		users := biz.Group("/users")
		users.GET("", middleware.Perm("system:user:list"), wrap(userH.List))
		users.GET("/:id", middleware.Perm("system:user:query"), wrap(userH.Get))
		users.POST("", middleware.Perm("system:user:create"), wrap(userH.Create))
		users.PUT("/:id", middleware.Perm("system:user:update"), wrap(userH.Update))
		users.PUT("/:id/password", middleware.Perm("system:user:update-password"), wrap(userH.ResetPassword))
		users.DELETE("/:id", middleware.Perm("system:user:delete"), wrap(userH.Delete))

		roles := biz.Group("/roles")
		roles.GET("", middleware.Perm("system:role:list"), wrap(roleH.List))
		roles.GET("/:id", middleware.Perm("system:role:query"), wrap(roleH.Get))
		roles.POST("", middleware.Perm("system:role:create"), wrap(roleH.Create))
		roles.PUT("/:id", middleware.Perm("system:role:update"), wrap(roleH.Update))
		roles.PUT("/:id/menus", middleware.Perm("system:role:update"), wrap(roleH.AssignMenus))
		roles.DELETE("/:id", middleware.Perm("system:role:delete"), wrap(roleH.Delete))

		menus := biz.Group("/menus")
		menus.GET("", middleware.Perm("system:menu:list"), wrap(menuH.List))
		menus.GET("/:id", middleware.Perm("system:menu:query"), wrap(menuH.Get))
		menus.POST("", middleware.Perm("system:menu:create"), wrap(menuH.Create))
		menus.PUT("/:id", middleware.Perm("system:menu:update"), wrap(menuH.Update))
		menus.DELETE("/:id", middleware.Perm("system:menu:delete"), wrap(menuH.Delete))

		depts := biz.Group("/depts")
		depts.GET("", middleware.Perm("system:dept:list"), wrap(deptH.List))
		depts.GET("/:id", middleware.Perm("system:dept:query"), wrap(deptH.Get))
		depts.POST("", middleware.Perm("system:dept:create"), wrap(deptH.Create))
		depts.PUT("/:id", middleware.Perm("system:dept:update"), wrap(deptH.Update))
		depts.DELETE("/:id", middleware.Perm("system:dept:delete"), wrap(deptH.Delete))

		posts := biz.Group("/posts")
		posts.GET("", middleware.Perm("system:post:list"), wrap(postH.List))
		posts.GET("/:id", middleware.Perm("system:post:query"), wrap(postH.Get))
		posts.POST("", middleware.Perm("system:post:create"), wrap(postH.Create))
		posts.PUT("/:id", middleware.Perm("system:post:update"), wrap(postH.Update))
		posts.DELETE("/:id", middleware.Perm("system:post:delete"), wrap(postH.Delete))

		// 平台路由：JWT → PlatformPerm（g2 平台管理员 + dom="*" Enforce）。
		// 不挂 Tenant 中间件：不注入租户 ctx，直接操作全局表 t_tenant；
		// platform:tenant:* 权限标识不进入菜单表（防租户超管越权，方案 6.3）。
		plat := logged.Group("/tenants")
		plat.GET("", middleware.PlatformPerm("platform:tenant:list"), wrap(tenantH.List))
		plat.GET("/:id", middleware.PlatformPerm("platform:tenant:query"), wrap(tenantH.Get))
		plat.POST("", middleware.PlatformPerm("platform:tenant:create"), wrap(tenantH.Create))
		plat.PUT("/:id", middleware.PlatformPerm("platform:tenant:update"), wrap(tenantH.Update))
		plat.PUT("/:id/status", middleware.PlatformPerm("platform:tenant:update"), wrap(tenantH.UpdateStatus))
		plat.DELETE("/:id", middleware.PlatformPerm("platform:tenant:delete"), wrap(tenantH.Delete))
	}
}

// wrap 适配 response.Handler → gin.HandlerFunc（统一错误出口）。
func wrap(h response.Handler) gin.HandlerFunc { return response.Wrap(h) }
