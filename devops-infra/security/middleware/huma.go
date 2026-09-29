// huma 版认证/鉴权中间件（D2/D7）。
//
// 决策在 infra，呈现靠 HumaOptions 回调注入——
// 默认回调以统一信封（code/msg/data）呈现 401/403/503，业务侧可注入文案定制。
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"devops-infra/http/response"
	securityauthz "devops-infra/security/authz"
	securityjwt "devops-infra/security/jwt"
)

// ErrNotPlatformAdmin 非平台管理员（供回调区分文案，行为仍为拒绝）。
var ErrNotPlatformAdmin = errors.New("middleware: not a platform admin")

// claimsContextKey claims 在 context.Context 中的键。
type claimsContextKey struct{}

// BearerAuthenticator 框架无关认证决策接口。
// *Authenticator 与业务门面（如 devops-system 的 auth.Manager）均实现此接口。
type BearerAuthenticator interface {
	Authenticate(ctx context.Context, bearerHeader string) (*securityjwt.Claims, error)
}

// HumaOptions 错误呈现回调（与 GinOptions 同构）。
type HumaOptions struct {
	OnUnauthorized        func(hctx huma.Context, err error)
	OnForbidden           func(hctx huma.Context, err error)
	OnInfrastructureError func(hctx huma.Context, err error)
}

// HumaCurrentClaims 从 context 取认证载荷（HumaAuthenticate 之后可用）。
func HumaCurrentClaims(ctx context.Context) *securityjwt.Claims {
	claims, _ := ctx.Value(claimsContextKey{}).(*securityjwt.Claims)
	return claims
}

// HumaAuthenticate huma 认证中间件：Bearer 解析 → token 解析 → 会话态；
// 通过后 claims 经 huma.WithValue 注入 context。
func HumaAuthenticate(authn BearerAuthenticator, opts HumaOptions) func(huma.Context, func(huma.Context)) {
	opts = withHumaDefaults(opts)
	return func(hctx huma.Context, next func(huma.Context)) {
		if authn == nil {
			opts.OnInfrastructureError(hctx, ErrUnauthorized)
			return
		}
		claims, err := authn.Authenticate(hctx.Context(), hctx.Header("Authorization"))
		if err != nil {
			if errors.Is(err, ErrInfrastructure) {
				opts.OnInfrastructureError(hctx, err)
			} else {
				opts.OnUnauthorized(hctx, err)
			}
			return // 不调 next 即中断
		}
		next(huma.WithValue(hctx, claimsContextKey{}, claims))
	}
}

// claimsOrReject claims 缺失时已写 401 并返回 false；业务中间件 fail-closed 守卫的公共收口。
func claimsOrReject(hctx huma.Context, opts HumaOptions) (*securityjwt.Claims, bool) {
	claims := HumaCurrentClaims(hctx.Context())
	if claims == nil {
		opts.OnUnauthorized(hctx, ErrUnauthorized)
		return nil, false
	}
	return claims, true
}

// HumaRequirePermission 域内鉴权（uid × tid × perm）。
// authorizer 未装配（nil）时 fail-closed 拒绝（与 gin 版行为一致）。
func HumaRequirePermission(authorizer securityauthz.Authorizer, permission string, opts HumaOptions) func(huma.Context, func(huma.Context)) {
	opts = withHumaDefaults(opts)
	return func(hctx huma.Context, next func(huma.Context)) {
		claims, ok := claimsOrReject(hctx, opts)
		if !ok {
			return
		}
		if authorizer == nil {
			opts.OnForbidden(hctx, ErrForbidden)
			return
		}
		ok, err := authorizer.Enforce(
			hctx.Context(),
			strconv.FormatInt(claims.UID, 10),
			strconv.FormatInt(claims.TenantID, 10),
			permission,
		)
		if err != nil {
			opts.OnInfrastructureError(hctx, err)
			return
		}
		if !ok {
			opts.OnForbidden(hctx, ErrForbidden)
			return
		}
		next(hctx)
	}
}

// HumaRequirePlatformPermission 平台管理员鉴权（g2 校验 + dom="*" Enforce）。
// 非平台管理员与 Enforce 拒绝均走 OnForbidden，以不同错误值区分文案。
func HumaRequirePlatformPermission(authorizer securityauthz.Authorizer, permission string, opts HumaOptions) func(huma.Context, func(huma.Context)) {
	opts = withHumaDefaults(opts)
	return func(hctx huma.Context, next func(huma.Context)) {
		claims, ok := claimsOrReject(hctx, opts)
		if !ok {
			return
		}
		if authorizer == nil {
			opts.OnForbidden(hctx, ErrForbidden)
			return
		}
		subject := strconv.FormatInt(claims.UID, 10)
		isAdmin, err := authorizer.IsPlatformAdmin(hctx.Context(), subject)
		if err != nil {
			opts.OnInfrastructureError(hctx, err)
			return
		}
		if !isAdmin {
			opts.OnForbidden(hctx, ErrNotPlatformAdmin)
			return
		}
		ok, err = authorizer.EnforcePlatform(hctx.Context(), subject, permission)
		if err != nil {
			opts.OnInfrastructureError(hctx, err)
			return
		}
		if !ok {
			opts.OnForbidden(hctx, ErrForbidden)
			return
		}
		next(hctx)
	}
}

// withHumaDefaults 默认回调以统一信封呈现（文案为 HTTP 状态短语），避免忘注入回调时返回无 body 的裸状态码。
func withHumaDefaults(opts HumaOptions) HumaOptions {
	if opts.OnUnauthorized == nil {
		opts.OnUnauthorized = func(hctx huma.Context, _ error) {
			response.WriteHumaEnvelope(hctx, http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))
		}
	}
	if opts.OnForbidden == nil {
		opts.OnForbidden = func(hctx huma.Context, _ error) {
			response.WriteHumaEnvelope(hctx, http.StatusForbidden, http.StatusText(http.StatusForbidden))
		}
	}
	if opts.OnInfrastructureError == nil {
		opts.OnInfrastructureError = func(hctx huma.Context, _ error) {
			response.WriteHumaEnvelope(hctx, http.StatusServiceUnavailable, http.StatusText(http.StatusServiceUnavailable))
		}
	}
	return opts
}
