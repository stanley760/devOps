// Package router 注册 devops-system 的业务路由。
//
// 分工：Huma 主导全部业务接口（operations.go 按模块 huma.Register，
// 含路由/校验/业务中间件/文档四合一）；Gin 仅为底层（HTTP 引擎、全局中间件、
// /healthz /readyz /metrics /jwks 基础端点，均由 devops-infra/http 与本文件装配）。
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	httpinfra "devops-infra/http"
	"devops-infra/http/apidoc"

	apiv1 "devops-system/internal/api/v1"
	"devops-system/internal/auth"
	"devops-system/internal/config"
	"devops-system/internal/middleware"
	"devops-system/internal/service"
)

// Register 返回业务路由注册回调，交给 httpinfra.Run 组装。
func Register(docCfg config.APIDocConfig, authM *auth.Manager, authSvc *service.Auth, userSvc *service.User,
	roleSvc *service.Role, menuSvc *service.Menu, deptSvc *service.Dept, postSvc *service.Post,
	tenantSvc *service.Tenant,
) httpinfra.Register {
	authH := apiv1.NewAuthHandler(authSvc)
	userH := apiv1.NewUserHandler(userSvc)
	roleH := apiv1.NewRoleHandler(roleSvc)
	menuH := apiv1.NewMenuHandler(menuSvc)
	deptH := apiv1.NewDeptHandler(deptSvc)
	postH := apiv1.NewPostHandler(postSvc)
	tenantH := apiv1.NewTenantHandler(tenantSvc)

	return func(r *gin.Engine) {
		// 验签公钥（RFC 7517 JWKS）：Higress jwt-auth 插件 jwksFromUrl / 服务自验签拉取
		r.GET("/.well-known/jwks.json", jwksHandler(authM))

		// huma API 恒装配（D5）：Enabled 只门控文档端点，路由注册不受影响。
		humaAPI := apidoc.New(r, apidoc.Options{
			Title:        "devops-system API",
			Version:      "v1",
			DocsPath:     docCfg.BasePath,
			BearerAuth:   true,
			TenantHeader: "X-Tenant-ID",
			Security:     []map[string][]string{{"bearerAuth": {}, "tenantId": {}}},
			Enabled:      docCfg.Enabled,
		})

		// 中间件套件（D7：infra 决策 + 本服务信封呈现，装配一次各模块复用）。
		kit := humaKit{
			jwtM:    middleware.HumaJWT(authM),
			tenantM: middleware.HumaTenant(),
			permM:   middleware.HumaPerm,
			platM:   middleware.HumaPlatformPerm,
		}

		// 全量业务注册（40 接口，auth 7 + users 6 + roles 6 + menus 5 + depts 5 + posts 5 + tenants 6）。
		registerAuth(humaAPI, authH, kit.jwtM, kit.tenantM)
		registerUsers(humaAPI, userH, kit)
		registerRoles(humaAPI, roleH, kit)
		registerMenus(humaAPI, menuH, kit)
		registerDepts(humaAPI, deptH, kit)
		registerPosts(humaAPI, postH, kit)
		registerTenants(humaAPI, tenantH, kit)
	}
}

// jwksHandler 输出 RS256 验签公钥的 JWKS 文档（仅公钥材料，n/e）。
func jwksHandler(m *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		doc, err := m.JWKS()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "jwks unavailable"})
			return
		}
		c.JSON(http.StatusOK, doc)
	}
}
