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
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"devops-id/pkg/idclient"
)

// idc 进程级发号客户端，由 cmd 装配期注入（devops-system → devops-id gRPC）。
var idc *idclient.Client

// SetIDGenerator 注入发号客户端（server 命令装配期调用；migrate 种子不依赖）。
func SetIDGenerator(c *idclient.Client) { idc = c }

// IDGenerator 返回当前发号客户端（未装配时为 nil）。
func IDGenerator() *idclient.Client { return idc }

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
	if m.ID != 0 { // 种子数据显式 ID，不消耗号段
		return nil
	}
	if idc == nil {
		return errors.New("ID 生成器未初始化（SetIDGenerator 未装配）")
	}
	tag, err := BizTagOf(tx.Statement.Table)
	if err != nil {
		return err
	}
	id, err := idc.Next(tx.Statement.Context, tag)
	if err != nil {
		return fmt.Errorf("ID 生成失败(biz_tag=%s): %w", tag, err)
	}
	m.ID = id
	return nil
}

// tableBizTag 表名 → biz_tag 显式映射（与 t_leaf_alloc 注册行一致，单数命名）。
// 关联表（user_role/role_menu）复合主键不走发号，不在此列；
// 新增业务表时须同步在 migrate 的 registerBizTags 注册号段，否则建行 fail-fast。
var tableBizTag = map[string]string{
	"t_system_users": "user",
	"t_system_role":  "role",
	"t_system_menu":  "menu",
	"t_system_dept":  "dept",
	"t_system_post":  "post",
	"t_tenant":       "tenant",
}

// BizTagOf 表名 → biz_tag（Leaf-segment 注册标识）。
func BizTagOf(table string) (string, error) {
	if tag, ok := tableBizTag[table]; ok {
		return tag, nil
	}
	return "", fmt.Errorf("表 %s 无对应 biz_tag（号段未注册）", table)
}
