package app

import (
	"github.com/stanley760/devOps/devops-system/internal/auth"
	"github.com/stanley760/devOps/devops-system/internal/middleware"
	"github.com/stanley760/devOps/devops-system/internal/service"
)

func newUserService(authM *auth.Manager, kit *middleware.Kit) *service.User {
	return service.NewUser(authM, kit.InvalidateDataScope)
}

func newRoleService(kit *middleware.Kit) *service.Role {
	return service.NewRole(kit.InvalidateDataScope)
}
