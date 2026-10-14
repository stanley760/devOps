package humakit

import (
	tenantinfra "github.com/stanley760/devops/devops-infra/tenant"
)

// securityTenant 租户校验 + 上下文注入（可用性判定经 Deps.TenantUsable 注入，
// 业务侧决定停用/过期语义与缓存策略）。
func (k *Kit) securityTenant() Middleware {
	return tenantinfra.HumaTenantContext(k.deps.TenantUsable,
		k.deps.Envelope.Unauthorized, k.deps.Envelope.TenantForbidden)
}
