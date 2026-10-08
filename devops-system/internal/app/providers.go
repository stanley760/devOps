//go:build wireinject

// Package app owns the application dependency graph shared by server entrypoints.
package app

import (
	"github.com/google/wire"

	"devops-system/internal/config"
	"devops-system/internal/router"
	"devops-system/internal/service"
)

var serviceSet = wire.NewSet(
	service.NewAuth,
	newUserService,
	newRoleService,
	service.NewMenu,
	service.NewDept,
	service.NewPost,
	service.NewTenant,
	service.NewLoginLog,
)

var routeSet = wire.NewSet(
	// NewRegister 的 docCfg 入参：wire 不自动解构注入器入参结构体，须显式提供字段 provider
	wire.FieldsOf(new(config.Config), "APIDoc"),
	router.NewAuthRoutes,
	router.NewUserRoutes,
	router.NewRoleRoutes,
	router.NewMenuRoutes,
	router.NewDeptRoutes,
	router.NewPostRoutes,
	router.NewTenantRoutes,
	router.NewLoginLogRoutes,
	wire.Struct(new(router.Modules), "*"),
	router.NewRegister,
)
