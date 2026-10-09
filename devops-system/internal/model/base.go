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
	"strings"
	"time"

	"gorm.io/gorm"

	"devops-id/pkg/idgorm"
	"devops-system/internal/auditctx"
)

// idHook 进程级发号钩子，由 cmd 装配期注入生成器。
// 默认 biz_tag 由具体模型的 Go 类型名推导；特殊命名通过 BizTagProvider 覆盖。
var idHook = idgorm.New(nil, nil)

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

// BeforeCreate 填充当前操作者并由 devops-id 分配主键（ID 为 0 时）。
// 无操作者时保留显式审计字段，供迁移/种子等系统写入使用。
func (m *BaseModel) BeforeCreate(tx *gorm.DB) error {
	actor, ok := auditctx.From(tx.Statement.Context)
    username := strings.TrimSpace(actor.Username)

    if ok && username != "" {
        // 可信认证用户覆盖调用方传入的值。
        m.Creator = username
        m.Updater = username
    } else {
        // 无认证操作者时，保留显式系统值，只补空字段。
        if m.Creator == "" {
            m.Creator = "system"
        }
        if m.Updater == "" {
            m.Updater = "system"
        }
    }
	return idHook.Assign(tx, &m.ID)
}

// BeforeUpdate overwrites updater from the authenticated request context.
func (m *BaseModel) BeforeUpdate(tx *gorm.DB) error {
	actor, ok := auditctx.From(tx.Statement.Context)
    username := strings.TrimSpace(actor.Username)
    if !ok || username == "" {
        username = "system"
    }

    tx.Statement.SetColumn("updater", username)
    return nil
}

// BizTagOf 模型 → biz_tag（独立 snowflake worker 空间标识）。
func BizTagOf(model any) (string, error) { return idHook.BizTagOf(model) }
