// TenantPlugin：GORM 回调自动注入/过滤 tenant_id。
//
// fail-closed 语义：
//   - stmt.Schema 不含 tenant_id 字段（全局表）→ 放行
//   - 原生 JOIN 查询（stmt.Joins 非空）→ 放行：dao 已显式过滤（裸列在多表下有歧义风险）
//   - ctx 无租户且未显式 Skip → 直接报错，查询失败（防串数据）
//   - 查询/改/删：注入 WHERE tenant_id = ?；新建：填充 TenantID 字段
//
// 仅在 server 装配期注册（migrate/种子直接走 db.Get()，无插件、不误伤）。
package tenantdb

import (
	"github.com/stanley760/devops/devops-infra/tenant"
	"gorm.io/gorm"
)

// RegisterPlugin 在共享连接上注册租户过滤回调（server 命令装配期调用一次）。
func RegisterPlugin(gdb *gorm.DB) error {
	return tenant.RegisterPlugin(gdb)
}
