// middleware 包的 CORS 组件：跨域请求统一处理。
//
// 设计要点：
//   - 白名单精确匹配（支持 "*" 通配全部源）；不在名单内的源不加 CORS 头，
//     由浏览器侧拦截（服务端不报错、不泄露存在性）
//   - 预检 OPTIONS 直接 204 结束，不进入业务路由（避免命中 405 信封）
//   - allow_credentials=true 时不回显 "*"（规范禁止），改为回显命中的具体源
//   - 未配置 allow_origins 的服务由 httpx 决定是否挂载（默认不挂，仅同源）
package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORSConfig CORS 配置（由 httpx 从 config.server.cors 装配）。
type CORSConfig struct {
	AllowOrigins     []string      // 允许的源，精确匹配；"*" 通配全部
	AllowMethods     []string      // 允许的方法，空则默认 GET/POST/PUT/DELETE/OPTIONS
	AllowHeaders     []string      // 允许的请求头，空则默认常用头
	AllowCredentials bool          // 允许携带 Cookie；为 true 时不回显 "*"
	MaxAge           time.Duration // 预检结果缓存时长（浏览器免重复预检）
}

// CORS 返回 CORS 中间件。
func CORS(cfg CORSConfig) gin.HandlerFunc {
	methods := joinOrDefault(cfg.AllowMethods, "GET, POST, PUT, DELETE, OPTIONS")
	headers := joinOrDefault(cfg.AllowHeaders, "Origin, Content-Type, Authorization, X-Requested-With")
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.Itoa(int(cfg.MaxAge.Seconds()))
	}
	wildcard := containsStr(cfg.AllowOrigins, "*")

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next() // 同源请求 / 服务端直调，无 CORS 语义
			return
		}

		if wildcard || containsStr(cfg.AllowOrigins, origin) {
			c.Header("Vary", "Origin")
			switch {
			case wildcard && !cfg.AllowCredentials:
				c.Header("Access-Control-Allow-Origin", "*")
			default: // 精确命中回显该源；通配 + credentials 场景规范禁止 "*"，同样回显具体源
				c.Header("Access-Control-Allow-Origin", origin)
			}
			if cfg.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		// 预检请求：无论是否放行都直接 204 结束（未放行的缺少 CORS 头，浏览器自行拦截）
		if c.Request.Method == http.MethodOptions {
			if wildcard || containsStr(cfg.AllowOrigins, origin) {
				c.Header("Access-Control-Allow-Methods", methods)
				c.Header("Access-Control-Allow-Headers", headers)
				if maxAge != "" {
					c.Header("Access-Control-Max-Age", maxAge)
				}
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// joinOrDefault 列表转逗号串；空则用默认值。
func joinOrDefault(list []string, def string) string {
	if len(list) == 0 {
		return def
	}
	return strings.Join(list, ", ")
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
