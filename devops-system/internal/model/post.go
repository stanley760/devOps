// t_system_post 岗位表（租户表）。
// 唯一性：uk_tenant_code(tenant_id,code) 由 migrate DDL 声明。
package model

// Post 岗位。
type Post struct {
	BaseTenantModel
	Code   string `gorm:"size:64;not null"`
	Name   string `gorm:"size:50;not null"`
	Sort   int
	Status int8   `gorm:"not null;default:1"` // 0停用 1正常
	Remark string `gorm:"size:500"`
}

// TableName 表名。
func (Post) TableName() string { return "t_system_post" }
