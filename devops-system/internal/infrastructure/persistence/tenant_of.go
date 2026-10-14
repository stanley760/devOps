package persistence

import (
	"context"

	"github.com/stanley760/devops/devops-infra/tenant"
)

// tenantOf 从上下文取租户 ID。
//
// 为什么需要它：涉及 JOIN 的查询或 Unscoped 查询会绕开租户插件的自动注入
// （见 devops-infra/tenant 插件的 JOIN 放行规则），此时必须显式带 tenant_id，
// 否则就是跨租户数据泄漏。取不到租户即 fail-closed 报错。
//
// fail-closed 语义与既有实现一致（dao/user.go:tenantOf、dao/menu.go:tenantOfCtx），
// 收敛为唯一实现。
func tenantOf(ctx context.Context) (int64, error) {
	return tenant.RequiredTenantID(ctx)
}
