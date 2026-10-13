// Package router 注册 devops-system 的业务路由。
//
// 分工：Huma 主导全部业务接口（operations.go 按模块 huma.Register，
// 含路由/校验/业务中间件/文档四合一）；Gin 仅为底层（HTTP 引擎、全局中间件、
// /healthz /readyz /metrics /jwks 基础端点，均由 devops-infra/http 与本文件装配）。
package router

import (
	"github.com/gin-gonic/gin"
	httpinfra "github.com/stanley760/devOps/devops-infra/http"
	"github.com/stanley760/devOps/devops-infra/http/apidoc"
	"github.com/stanley760/devOps/devops-system/internal/auth"
	"github.com/stanley760/devOps/devops-system/internal/config"
	"net/http"
)

// NewRegister 组合应用层提供的业务模块路由，返回交给 httpinfra.Run 的注册回调。
// 模块及其依赖由 internal/app 装配；infra 不依赖任何业务类型。
func NewRegister(docCfg config.APIDocConfig, authM *auth.Manager, modules Modules) httpinfra.Register {
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

		// 各业务模块拥有自己的 Handler、Service 和中间件依赖。
		modules.Auth(humaAPI)
		modules.Users(humaAPI)
		modules.Roles(humaAPI)
		modules.Menus(humaAPI)
		modules.Depts(humaAPI)
		modules.Posts(humaAPI)
		modules.Tenants(humaAPI)
		modules.LoginLogs(humaAPI)
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
