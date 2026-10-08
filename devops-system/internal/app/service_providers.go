package app

import (
	"devops-system/internal/auth"
	"devops-system/internal/middleware"
	"devops-system/internal/service"
)

func newUserService(authM *auth.Manager, kit *middleware.Kit) *service.User {
	return service.NewUser(authM, kit.InvalidateDataScope)
}

func newRoleService(kit *middleware.Kit) *service.Role {
	return service.NewRole(kit.InvalidateDataScope)
}
