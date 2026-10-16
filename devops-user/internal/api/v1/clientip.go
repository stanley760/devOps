package v1

import (
	"context"
	"net"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humagin "github.com/danielgtaylor/huma/v2/adapters/humagin"
)

type clientIPKey struct{}

// ClientIPMiddleware 把客户端 IP 注入 context，供限流等下游使用。
//
// 为什么不让 handler 直接读 huma.Context：handler 签名是 func(ctx, in)，
// 拿不到 huma.Context。中间件在此处桥接。
//
// 取值方式与 devops-infra/security/humakit 的审计实现一致：
//   - gin 适配器下用 g.ClientIP()，它按 server.trusted_proxies 校验
//     X-Forwarded-For——**不在可信列表内的代理头会被忽略**，否则攻击者
//     可伪造 XFF 绕过按 IP 的登录限流；
//   - 非 gin 适配器回退为直读 XFF 首段（此时无代理校验能力）。
func ClientIPMiddleware(hctx huma.Context, next func(huma.Context)) {
	ip := resolveClientIP(hctx)
	next(huma.WithValue(hctx, clientIPKey{}, ip))
}

// ClientIPFrom 从 context 取客户端 IP（未经过中间件时返回空串）。
func ClientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

// resolveClientIP 解析客户端 IP。
func resolveClientIP(hctx huma.Context) string {
	if g := humagin.Unwrap(hctx); g != nil {
		ip := g.ClientIP()
		// ClientIP 在无任何来源时返回空串；此时回退到对端地址字符串。
		if ip != "" {
			return ip
		}
		return remoteIP(hctx)
	}
	// 非 gin：直读 XFF 首段。注意此路径不校验代理身份，仅在无 gin 适配器时使用。
	xff := hctx.Header("X-Forwarded-For")
	if idx := strings.IndexByte(xff, ','); idx > 0 {
		xff = xff[:idx]
	}
	if ip := strings.TrimSpace(xff); ip != "" {
		return ip
	}
	return remoteIP(hctx)
}

// remoteIP 取对端地址（去掉端口）。
func remoteIP(hctx huma.Context) string {
	addr := hctx.RemoteAddr()
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
