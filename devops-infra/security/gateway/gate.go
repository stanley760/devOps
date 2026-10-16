// Package gateway 提供「流量必须经由网关进入」的应用层强制守卫。
//
package gateway

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	commonResp "github.com/stanley760/devops/devops-common/response"
	securitymw "github.com/stanley760/devops/devops-infra/security/middleware"
)

// HeaderTenant 网关 jwt-auth 的 claims_to_headers 注入的租户头（override 语义）。
// 本守卫只做「必须携带有效 JWT」的收口，不信任该头——租户身份一律以 token claims 为准。
const HeaderTenant = "X-Tenant-ID"

// Options 守卫配置。
type Options struct {
	// RequireJWT 为 true 时启用守卫；false 时直通。
	RequireJWT bool
	// Authenticator 提供 Bearer JWT 的独立校验（签名 + 会话态）。
	// 为 nil 且 RequireJWT 为 true 时 fail-closed。
	Authenticator securitymw.BearerAuthenticator
	// ExemptPaths 为免校验路径（精确匹配），如健康检查与指标端点。
	ExemptPaths []string
	// ExemptPrefixes 为免校验路径前缀（精确匹配），如 /apidoc。
	ExemptPrefixes []string
}

// Config 由业务配置段映射而来，便于按环境开关。
type Config struct {
	// RequireJWT 为 true 时启用守卫；false 时直通。
	RequireJWT bool `mapstructure:"require_jwt"`
	// ExemptPaths 追加免校验的精确路径。
	ExemptPaths []string `mapstructure:"exempt_paths"`
}

// Enforce 返回流量入口守卫中间件：要求请求携带可独立校验通过的 Bearer JWT。
//
// requireJWT 为 false 时返回直通中间件，使未启用守卫的环境（本地 go run、单测）
// 无需任何额外配置，且不会因为漏配导致全站 401。
func Enforce(requireJWT bool, authn securitymw.BearerAuthenticator, exemptPaths, exemptPrefixes []string) gin.HandlerFunc {
	if !requireJWT {
		slog.Info("网关守卫未启用（gateway.require_jwt=false），流量入口不做强制校验")
		return func(c *gin.Context) { c.Next() }
	}
	if authn == nil {
		// fail-closed：守卫已声明启用却没装配校验器时必须拒绝，
		// 否则等于全站放行，收口形同虚设且无任何告警。
		slog.Error("网关守卫已启用但未装配 JWT 校验器，将拒绝全部非放行请求")
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, commonResp.BaseResponse[any]{
				Code: http.StatusUnauthorized,
				Msg:  "请求必须经由网关访问",
			})
		}
	}

	exact := make(map[string]struct{}, len(exemptPaths))
	for _, p := range exemptPaths {
		if p != "" {
			exact[p] = struct{}{}
		}
	}
	prefixes := make([]string, 0, len(exemptPrefixes))
	for _, p := range exemptPrefixes {
		if p != "" {
			prefixes = append(prefixes, p)
		}
	}

	// 预编译放行判定，避免每个请求重复遍历前缀。
	isExempt := func(path string) bool {
		if _, ok := exact[path]; ok {
			return true
		}
		for _, p := range prefixes {
			if len(path) >= len(p) && path[:len(p)] == p {
				return true
			}
		}
		return false
	}

	return func(c *gin.Context) {
		if isExempt(c.Request.URL.Path) {
			c.Next()
			return
		}

		// 不信任网关注入的租户头：租户身份一律以 token claims 为准，
		// 避免网关配置失误或被绕过时出现越权。此处仅记录，便于排查注入是否生效。
		_, err := authn.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
		if err != nil {
			// WARN 而非 ERROR：多半是绕过网关的直连流量（内部探活未配网关、
			// 运维误操作），可疑但未必是攻击。记录足以定位的字段，不记录凭据本身。
			slog.Warn("拒绝未经有效身份凭据的请求",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"client_ip", c.ClientIP(),
				"gateway_tenant", c.GetHeader(HeaderTenant),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, commonResp.BaseResponse[any]{
				Code: http.StatusUnauthorized,
				Msg:  "请求必须经由网关访问",
			})
			return
		}
		c.Next()
	}
}

// EnforceFromConfig 由业务配置装配守卫。RequireJWT 为 false 时直通。
func EnforceFromConfig(cfg Config, authn securitymw.BearerAuthenticator, exemptPaths, exemptPrefixes []string) gin.HandlerFunc {
	return Enforce(cfg.RequireJWT, authn, append(append([]string{}, exemptPaths...), cfg.ExemptPaths...), exemptPrefixes)
}