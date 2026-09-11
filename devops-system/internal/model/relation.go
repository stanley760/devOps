// 关联表（租户表）：用户-角色、角色-菜单。
// 复合主键、无独立 ID、不走发号；物理删除（无 deleted_at）——
// 分配/取消分配是幂等操作，软删会使复合主键无法重建同组合行。
package model

// UserRole 用户-角色关联。
type UserRole struct {
	TenantID int64 `gorm:"primaryKey"`
	UserID   int64 `gorm:"primaryKey;index:idx_user_role_user"`
	RoleID   int64 `gorm:"primaryKey;index:idx_user_role_role"`
}

// TableName 表名。
func (UserRole) TableName() string { return "t_system_user_role" }

// RoleMenu 角色-菜单关联（菜单全局，但授权按租户的角色发生）。
type RoleMenu struct {
	TenantID int64 `gorm:"primaryKey"`
	RoleID   int64 `gorm:"primaryKey;index:idx_role_menu_role"`
	MenuID   int64 `gorm:"primaryKey;index:idx_role_menu_menu"`
}

// TableName 表名。
func (RoleMenu) TableName() string { return "t_system_role_menu" }
