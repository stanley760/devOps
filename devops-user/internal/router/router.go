// Package router 注册 devops-user（C 端）的业务路由。
//
// 分工：Huma 主导全部接口（operations.go 按模块 huma.Register，含路由/校验/
// 业务中间件/文档四合一）；Gin 仅为底层（HTTP 引擎、全局中间件、/healthz
// /readyz /metrics /jwks 基础端点，均由 devops-infra/http 与本文件装配）。
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	httpinfra "github.com/stanley760/devops/devops-infra/http"
	"github.com/stanley760/devops/devops-infra/http/apidoc"

	"github.com/stanley760/devops/devops-user/internal/auth"
	"github.com/stanley760/devops/devops-user/internal/config"
)

// NewRegister 组合应用层提供的业务模块路由，返回交给 httpinfra.Run 的注册回调。
// 模块及其依赖由 cmd/root.go 装配；本包不依赖任何业务实现类型。
func NewRegister(docCfg config.APIDocConfig, authM *auth.Manager, modules Modules) httpinfra.Register {
	return func(r *gin.Engine) {
		// 验签公钥（RFC 7517 JWKS）：供其他服务自验签与人工排查。
		// 注意 Higress jwt-auth 没有 jwksFromUrl 字段，公钥是内联在其插件 yaml 里的，
		// 本端点对网关验签无用（wasm-go 实现可用 remote_jwks 指向它自动跟随轮换）。
		r.GET(JWKSPath, jwksHandler(authM))

		// huma API 恒装配：Enabled 只门控文档端点，业务路由注册不受影响。
		humaAPI := apidoc.New(r, apidoc.Options{
			Title:      "devops-user API",
			Version:    "v1",
			DocsPath:   docCfg.BasePath,
			BearerAuth: true,
			Security:   []map[string][]string{{"bearerAuth": {}}},
			Enabled:    docCfg.Enabled,
		})

		// 各业务模块拥有自己的 Handler、Service 与中间件依赖。
		modules.Auth(humaAPI)
		modules.Profile(humaAPI)
		modules.OAuth(humaAPI)
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
