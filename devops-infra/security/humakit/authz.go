package humakit

import (
	securitymw "devops-infra/security/middleware"
)

// securityPerm 域内鉴权（uid × tid × perm）。
func (k *Kit) securityPerm(perm string) Middleware {
	return securitymw.HumaRequirePermission(k.deps.Authorizer, perm, securitymw.HumaOptions{
		OnUnauthorized:        k.deps.Envelope.Unauthorized,
		OnForbidden:           k.deps.Envelope.PermForbidden,
		OnInfrastructureError: k.deps.Envelope.AuthzInfra,
	})
}

// securityPlat 平台鉴权（g2 + dom="*"）。
func (k *Kit) securityPlat(perm string) Middleware {
	return securitymw.HumaRequirePlatformPermission(k.deps.Authorizer, perm, securitymw.HumaOptions{
		OnUnauthorized:        k.deps.Envelope.Unauthorized,
		OnForbidden:           k.deps.Envelope.PlatformForbidden,
		OnInfrastructureError: k.deps.Envelope.AuthzInfra,
	})
}
