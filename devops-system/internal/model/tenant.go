// t_tenant 租户注册表（全局表，无 tenant_id）。
// code 全局唯一：前端租户标识与登录入参。
package model

import "time"

// Tenant 租户。
type Tenant struct {
	BaseModel
	Name          string     `gorm:"size:30;not null"`
	Code          string     `gorm:"size:30;not null;uniqueIndex:uk_code"`
	ContactName   string     `gorm:"size:30;column:contact_name"`
	ContactMobile string     `gorm:"size:500;column:contact_mobile"`
	Status        int8       `gorm:"not null"` // 0停用 1正常
	ExpireTime    *time.Time // NULL 永不过期
	Remark        string     `gorm:"size:500"`
}

// TableName 表名。
func (Tenant) TableName() string { return "t_tenant" }
