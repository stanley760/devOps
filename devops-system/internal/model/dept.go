// t_system_dept 部门表（租户表）。树结构内存组装（无 ancestors 列，对齐 yudao）。
package model

// Dept 部门。
type Dept struct {
	BaseTenantModel
	Name         string `gorm:"size:30;not null"`
	ParentID     int64  `gorm:"index;default:0"`
	Sort         int
	LeaderUserID int64  `gorm:"column:leader_user_id"`
	Phone        string `gorm:"size:20"`
	Email        string `gorm:"size:50"`
	Status       int8   `gorm:"not null;default:1"` // 0停用 1正常
}

// TableName 表名。
func (Dept) TableName() string { return "t_system_dept" }
