// Package gateway 提供「流量必须经由网关进入」的应用层强制守卫。
//
// 背景：Higress 只是可绕过的旁路边由。仅靠 compose 的 expose（不发布宿主机端口）
// 无法阻止同一 docker network 内的其他容器直连业务端口；即使限定为 K8s，Pod 内
// 任意进程也能直连。docker compose 形态更无 NetworkPolicy 可用。因此在应用层做
// 一次强制校验，使「未经过网关的直连请求」在进入业务逻辑前即被拒绝。
//
// 机制：网关侧以 override 语义注入 X-Gateway-Token（覆盖客户端同名头，见
// deploy/higress/wasmplugins/gateway-identity.yaml），本中间件用常量时间比较校验。
// 客户端自行携带该头无效——网关侧的覆盖语义保证到达这里的值只可能来自网关。
//
// 不变式：
//   - secret 为空时中间件直通（不启用守卫），保持 dev/测试形态零改动；
//   - 校验失败一律 403，且不泄露原因（避免探测者区分「头缺失」与「头错误」）；
//   - 放行名单内的端点（健康检查、Prometheus 抓取等）不经网关，仍可直连。
package gateway

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	commonResp "github.com/stanley760/devops/devops-common/response"
)

// HeaderToken 网关注入的可信身份头。客户端同名头会被网关覆盖，故不可伪造。
const HeaderToken = "X-Gateway-Token"

// Options 守卫配置。
type Options struct {
	// Secret 为网关与应用共享的密钥。空串表示不启用守卫（直通）。
	Secret string
	// ExemptPaths 为免校验路径（精确匹配），如健康检查与指标端点。
	ExemptPaths []string
	// ExemptPrefixes 为免校验路径前缀（精确匹配），如 /apidoc。
	ExemptPrefixes []string
}

// Config 由业务配置段映射而来，便于按环境开关。
type Config struct {
	// RequireToken 为 true 时启用守卫；false 时直通。
	RequireToken bool `mapstructure:"require_token"`
	// Token 为共享密钥，生产必须由密钥管理注入，禁止提交真实值。
	Token string `mapstructure:"token"`
	// ExemptPaths 追加免校验的精确路径。
	ExemptPaths []string `mapstructure:"exempt_paths"`
}

// Enforce 返回流量入口守卫中间件。
//
// secret 为空时返回直通中间件：这样未启用守卫的环境（本地 go run、单测）
// 无需任何额外配置，且不会因为漏配密钥导致全站 403。
func Enforce(secret string, exemptPaths, exemptPrefixes []string) gin.HandlerFunc {
	if secret == "" {
		slog.Info("网关守卫未启用（未配置共享密钥），流量入口不做强制校验")
		return func(c *gin.Context) { c.Next() }
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

		got := c.GetHeader(HeaderToken)
		// 常量时间比较：避免按字节短路带来的时序侧信道。
		if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			// WARN 而非 ERROR：这是绕过网关的流量，属可疑但未必是攻击
			// （可能是运维误操作、内部探活配错）。记录足以定位的字段。
			slog.Warn("拒绝未经网关的请求",
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"client_ip", c.ClientIP(),
			)
			c.AbortWithStatusJSON(http.StatusForbidden, commonResp.BaseResponse[any]{
				Code: http.StatusForbidden,
				Msg:  "请求必须经由网关访问",
			})
			return
		}
		c.Next()
	}
}

// EnforceFromConfig 由业务配置装配守卫。RequireToken 为 false 或 Token 为空时直通。
func EnforceFromConfig(cfg Config, exemptPaths, exemptPrefixes []string) gin.HandlerFunc {
	if !cfg.RequireToken {
		return Enforce("", nil, nil)
	}
	return Enforce(cfg.Token, append(append([]string{}, exemptPaths...), cfg.ExemptPaths...), exemptPrefixes)
}
