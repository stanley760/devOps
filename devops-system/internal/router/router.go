// Package router 注册 devops-system 的业务路由。
//
// /healthz 与统一中间件（Recovery/AccessLog/404/405 信封）由 devops-infra/httpx 提供，
// 这里只挂载业务路由；/api/v1 各模块随 P3-P8 逐步注册。
package router

import (
	"github.com/gin-gonic/gin"

	"devops-infra/httpx"
)

// Register 返回业务路由注册回调，交给 httpx.Run 组装。
func Register() httpx.Register {
	return func(r *gin.Engine) {
		// 业务路由随 P3-P8 逐步挂载：
		//   /api/v1/auth/*   认证（login/refresh/logout/info/routers/profile）
		//   /api/v1/users    用户管理
		//   /api/v1/roles    角色管理
		//   /api/v1/menus    菜单管理
		//   /api/v1/depts    部门管理
		//   /api/v1/posts    岗位管理
		//   /api/v1/tenants  租户管理（平台级）
	}
}
