// t_system_role 角色表（租户表）。
// 唯一性：uk_tenant_code(tenant_id,code) 由 internal/dbinit/sql/schema 建表 SQL 声明。
package model

// Role 角色。data_scope/data_scope_dept_ids 仅存储，行级数据权限列 Roadmap。
type Role struct {
	BaseTenantModel
	Name             string `gorm:"size:30;not null"`
	Code             string `gorm:"size:100;not null"`
	Sort             int
	DataScope        int8   `gorm:"column:data_scope;not null;default:1"` // 1全部 2自定义…
	DataScopeDeptIDs string `gorm:"size:500;column:data_scope_dept_ids"`
	Status           int8   `gorm:"not null"`           // 0停用 1正常
	Type             int8   `gorm:"not null;default:2"` // 1内置 2自定义
	Remark           string `gorm:"size:500"`
}

// TableName 表名。
func (Role) TableName() string { return "t_system_role" }
