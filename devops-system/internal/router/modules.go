package router

import (
	"github.com/danielgtaylor/huma/v2"

	apiv1 "github.com/stanley760/devOps/devops-system/internal/api/v1"
	"github.com/stanley760/devOps/devops-system/internal/middleware"
	"github.com/stanley760/devOps/devops-system/internal/service"
)

type AuthRoutes func(huma.API)
type UserRoutes func(huma.API)
type RoleRoutes func(huma.API)
type MenuRoutes func(huma.API)
type DeptRoutes func(huma.API)
type PostRoutes func(huma.API)
type TenantRoutes func(huma.API)
type LoginLogRoutes func(huma.API)
type OperationLogRoutes func(huma.API)

// Modules 是应用层需要显式组合的业务路由模块集合。
type Modules struct {
	Auth          AuthRoutes
	Users         UserRoutes
	Roles         RoleRoutes
	Menus         MenuRoutes
	Depts         DeptRoutes
	Posts         PostRoutes
	Tenants       TenantRoutes
	LoginLogs     LoginLogRoutes
	OperationLogs OperationLogRoutes
}

func NewAuthRoutes(svc *service.Auth, kit *middleware.Kit) AuthRoutes {
	h := apiv1.NewAuthHandler(svc)
	return func(api huma.API) { registerAuth(api, h, kit) }
}

func NewUserRoutes(svc *service.User, kit *middleware.Kit) UserRoutes {
	h := apiv1.NewUserHandler(svc)
	return func(api huma.API) { registerUsers(api, h, kit) }
}

func NewRoleRoutes(svc *service.Role, kit *middleware.Kit) RoleRoutes {
	h := apiv1.NewRoleHandler(svc)
	return func(api huma.API) { registerRoles(api, h, kit) }
}

func NewMenuRoutes(svc *service.Menu, kit *middleware.Kit) MenuRoutes {
	h := apiv1.NewMenuHandler(svc)
	return func(api huma.API) { registerMenus(api, h, kit) }
}

func NewDeptRoutes(svc *service.Dept, kit *middleware.Kit) DeptRoutes {
	h := apiv1.NewDeptHandler(svc)
	return func(api huma.API) { registerDepts(api, h, kit) }
}

func NewPostRoutes(svc *service.Post, kit *middleware.Kit) PostRoutes {
	h := apiv1.NewPostHandler(svc)
	return func(api huma.API) { registerPosts(api, h, kit) }
}

func NewTenantRoutes(svc *service.Tenant, kit *middleware.Kit) TenantRoutes {
	h := apiv1.NewTenantHandler(svc)
	return func(api huma.API) { registerTenants(api, h, kit) }
}

func NewLoginLogRoutes(svc *service.LoginLog, kit *middleware.Kit) LoginLogRoutes {
	h := apiv1.NewLoginLogHandler(svc)
	return func(api huma.API) { registerLoginLogs(api, h, kit) }
}

func NewOperationLogRoutes(svc *service.OperationLog, kit *middleware.Kit) OperationLogRoutes {
	h := apiv1.NewOperationLogHandler(svc)
	return func(api huma.API) { registerOperationLogs(api, h, kit) }
}
