// 跨嵌入字段的复合唯一索引（租户内唯一，方案 3 节）。
// GORM tag 无法在共享的 BaseTenantModel.TenantID 上按表组合，故以 DDL 声明；
// migrate 与服务启动共用。
package db

import (
	"fmt"

	"gorm.io/gorm"
)

// compositeUniques 租户内复合唯一索引清单。
var compositeUniques = []struct {
	Table  string
	Name   string
	Column string
}{
	{"t_system_users", "uk_tenant_username", "tenant_id, username"},
	{"t_system_users", "uk_tenant_mobile", "tenant_id, mobile"},
	{"t_system_role", "uk_tenant_code", "tenant_id, code"},
	{"t_system_post", "uk_tenant_code", "tenant_id, code"},
}

// EnsureCompositeUniques 逐个检查并创建（information_schema 判存，MySQL/MariaDB 通用）。
// 表不存在时跳过。
func EnsureCompositeUniques(gdb *gorm.DB) error {
	for _, u := range compositeUniques {
		var exists int64
		if err := gdb.Raw(
			"SELECT COUNT(*) FROM information_schema.tables "+
				"WHERE table_schema = DATABASE() AND table_name = ?",
			u.Table,
		).Scan(&exists).Error; err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		var cnt int64
		if err := gdb.Raw(
			"SELECT COUNT(*) FROM information_schema.statistics "+
				"WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
			u.Table, u.Name,
		).Scan(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			continue
		}
		if err := gdb.Exec(
			fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", u.Name, u.Table, u.Column),
		).Error; err != nil {
			return fmt.Errorf("表 %s 创建 %s 失败: %w", u.Table, u.Name, err)
		}
	}
	return nil
}
