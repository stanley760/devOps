package humakit

import (
	"github.com/danielgtaylor/huma/v2"

	securitymw "github.com/stanley760/devops/devops-infra/security/middleware"
)

// securityJWT huma 认证中间件：infra 决策 + 本套件信封呈现 + 可选 ctx 增强。
// claims 经 securitymw 注入；认证成功后调 Deps.WithClaims（审计 actor 等业务增强）。
func (k *Kit) securityJWT() Middleware {
	authenticate := securitymw.HumaAuthenticate(k.authn, securitymw.HumaOptions{
		OnUnauthorized:        k.deps.Envelope.Unauthorized,
		OnInfrastructureError: k.deps.Envelope.AuthnInfra,
	})
	withClaims := k.deps.WithClaims
	return func(hctx huma.Context, next func(huma.Context)) {
		authenticate(hctx, func(authenticated huma.Context) {
			if withClaims != nil {
				claims := securitymw.HumaCurrentClaims(authenticated.Context())
				if claims != nil {
					ctx := withClaims(authenticated.Context(), claims)
					authenticated = huma.WithContext(authenticated, ctx)
				}
			}
			next(authenticated)
		})
	}
}
