// t_system_users 用户表（租户表）。
// 唯一性：uk_tenant_username(tenant_id,username)、uk_tenant_mobile(tenant_id,mobile)
// —— 跨嵌入字段的复合唯一索引由 internal/dbinit/sql/schema 建表 SQL 声明（GORM tag 无法跨嵌入组合）。
package model

import "time"

// User 用户。
type User struct {
	BaseTenantModel
	Username  string     `gorm:"size:30;not null"`
	Password  string     `gorm:"size:100;not null"` // bcrypt
	Nickname  string     `gorm:"size:30;not null"`
	Remark    string     `gorm:"size:500"`
	DeptID    int64      `gorm:"index"`
	PostIDs   string     `gorm:"type:json;default:null"` // 岗位 ID JSON 数组，如 [4]；空值存 NULL（json_valid 约束拒绝 ''）
	Email     string     `gorm:"size:50"`
	Mobile    *string    `gorm:"size:20"`            // 指针：未设置存 NULL，保证 uk_tenant_mobile 多空值不冲突
	Gender    int8       `gorm:"not null;default:0"` // 0未知 1男 2女
	Avatar    string     `gorm:"size:512"`
	Status    int8       `gorm:"not null"` // 0停用 1正常
	LoginIP   string     `gorm:"size:50;column:login_ip"`
	LoginDate *time.Time `gorm:"column:login_date"`
}

// TableName 表名。
func (User) TableName() string { return "t_system_users" }
