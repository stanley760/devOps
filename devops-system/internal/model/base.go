// Package model 定义 devops-system 的全部表模型（t_tenant + t_system_*）。
//
// 两类基模型（方案 5 节）：
//   - BaseModel        全局表内嵌（t_tenant、t_system_menu）
//   - BaseTenantModel  租户表内嵌 = BaseModel + tenant_id 判别列
//
// 主键一律由 devops-id 发号：autoIncrement:false + BeforeCreate hook；
// 种子数据显式指定 ID（admin=1、部门=100 起、菜单 1-7 等），ID 为 0 时才自动发号。
package model

import (
	"time"

	"gorm.io/gorm"

	"devops-id/pkg/idgorm"
)

// tableBizTag 表名 → biz_tag 显式映射（单数命名）。
// 关联表（user_role/role_menu）复合主键不走发号，不在此列；
// 新增业务表时须同步配置映射，否则建行 fail-fast。
var tableBizTag = map[string]string{
	"t_system_users":      "user",
	"t_system_role":       "role",
	"t_system_menu":       "menu",
	"t_system_dept":       "dept",
	"t_system_post":       "post",
	"t_tenant":            "tenant",
	"t_system_login_log":  "login_log",
}

// idHook 进程级发号钩子，由 cmd 装配期注入生成器。
var idHook = idgorm.New(nil, tableBizTag)

// SetIDGenerator 注入发号客户端（server 命令装配期调用；migrate 种子不依赖）。
func SetIDGenerator(generator idgorm.Generator) { idHook.SetGenerator(generator) }

// IDGenerator 返回当前发号客户端（未装配时为 nil）。
func IDGenerator() idgorm.Generator { return idHook.Generator() }

// BaseModel 全局表基模型：主键 + 审计字段 + 软删（GORM 惯例 deleted_at）。
type BaseModel struct {
	ID         int64          `gorm:"primaryKey;autoIncrement:false"`
	Creator    string         `gorm:"size:64"`
	CreateTime time.Time      `gorm:"autoCreateTime"`
	Updater    string         `gorm:"size:64"`
	UpdateTime time.Time      `gorm:"autoUpdateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

// BaseTenantModel 租户表基模型：模式A 共享 Schema 的 tenant_id 判别列。
type BaseTenantModel struct {
	BaseModel
	TenantID int64 `gorm:"index;not null"`
}

// BeforeCreate 主键统一由 devops-id 发号（ID 为 0 时）。
// 事务内失败 → Create 整体回滚，绝不落库半行。
func (m *BaseModel) BeforeCreate(tx *gorm.DB) error {
	return idHook.Assign(tx, &m.ID)
}

// BizTagOf 表名 → biz_tag（独立 snowflake worker 空间标识）。
func BizTagOf(table string) (string, error) { return idHook.BizTagOf(table) }
