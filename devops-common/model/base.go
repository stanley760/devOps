// Package model 提供跨服务的 GORM 表基模型。
//
// ## 为什么放在这里
//
// devops-system 与 devops-user 都需要同一套「主键 + 审计字段 + 软删 + 发号」
// 的基模型。放在 devops-common 而非各自internal，是为了让两者共用**同一份实现**
// ——此前两份BaseModel 并存，hook 逻辑一旦要改必须改两处，极易漏。
//
// ## 依赖边界（重要）
//
// 本包**不依赖 devops-id**：devops-id 是下游业务模块，common 不能反向依赖它。
// 故发号抽象为 IDAssigner 接口，由各服务在装配期注入 devops-id 的实现。
//
// ## 审计操作者的两种语义
//
//	ActorResolver 返回 nil → 固定填ActorFallback（"system"）；
//	ActorResolver 返回非nil → 填该操作者（认证用户名等）。
//
// devops-system 注入 auditctx 解析（登录后台的用户名）；
// devops-user 注入 nil（注册时无认证操作者，固定 system）。
package model

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// ActorFallback 无认证操作者时写入 creator/updater 的默认值。
const ActorFallback = "system"

// IDAssigner 主键发号器抽象。
//
// 实现方通常是 devops-id 的客户端（devops-id/pkg/idgrom 的 Generator），
// 由各服务在装配期注入，避免本包依赖 devops-id。
type IDAssigner interface {
	// Assign 为dest 分配主键；已是非零值时保持不变（供种子数据显式指定 ID）。
	Assign(tx *gorm.DB, dest *int64) error
}

// ActorResolver 返回当前请求的操作者名；返回空串表示无认证操作者。
type ActorResolver func(tx *gorm.DB) string

// config 进程级装配参数，由 Configure 注入。
//
// 包级变量而非结构体字段：GORM 的 BeforeCreate/BeforeUpdate 钩子由内嵌的
// BaseModel 提供，拿不到外层实例，故只能用进程级配置。
var (
	idAssigner IDAssigner
	// actorResolver 可为 nil（表示固定用 ActorFallback）。
	actorResolver ActorResolver
)

// Configure 注入发号器与操作者解析器。装配期调用一次（server 命令）。
//
// actorResolver 为 nil 时 BeforeCreate/BeforeUpdate 固定填 ActorFallback。
// 必须在任何写库操作之前调用；未调用时发号器为 nil，插入会失败（fail-closed，
// 避免静默生成 0 号主键）。
func Configure(assigner IDAssigner, resolver ActorResolver) {
	idAssigner = assigner
	actorResolver = resolver
}

// SetIDGenerator 便捷入口：仅注入发号器，保留现有 ActorResolver。
func SetIDGenerator(assigner IDAssigner) { idAssigner = assigner }

// CurrentIDAssigner 返回当前发号器（未装配时为 nil）。
func CurrentIDAssigner() IDAssigner { return idAssigner }

// BaseModel 全局表基模型：主键 + 审计字段 + 软删（GORM 惯例 deleted_at）。
//
// 供各服务的表模型内嵌：
//
//	type User struct {
//	    model.BaseModel
//	    Username string `gorm:"size:32;not null"`
//	}
type BaseModel struct {
	ID         int64          `gorm:"primaryKey;autoIncrement:false"`
	Creator    string         `gorm:"size:64"`
	CreateTime time.Time      `gorm:"autoCreateTime"`
	Updater    string         `gorm:"size:64"`
	UpdateTime time.Time      `gorm:"autoUpdateTime"`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

// BeforeCreate 填充操作者并分配主键（ID 为 0 时才发号）。
//
// 覆盖语义（**安全相关，不可简化**）：
//   - 有认证操作者 → **无条件覆盖** Creator/Updater。调用方传入的值不可信
//     （可能来自请求体），必须以认证身份为准，否则审计字段可被伪造。
//   - 无认证操作者 → **仅补空字段**，保留调用方的显式值。
//     种子数据靠这个保留 admin="system" 之类的显式指定。
func (m *BaseModel) BeforeCreate(tx *gorm.DB) error {
	actor := ""
	if actorResolver != nil {
		actor = strings.TrimSpace(actorResolver(tx))
	}
	if actor != "" {
		m.Creator = actor
		m.Updater = actor
	} else {
		if m.Creator == "" {
			m.Creator = ActorFallback
		}
		if m.Updater == "" {
			m.Updater = ActorFallback
		}
	}
	return assignID(tx, &m.ID)
}

// BeforeUpdate 覆盖 updater。
//
// 同BeforeCreate：有认证操作者时无条件覆盖（防止请求体里的 updater 伪造审计）。
func (m *BaseModel) BeforeUpdate(tx *gorm.DB) error {
	actor := ActorFallback
	if actorResolver != nil {
		if name := strings.TrimSpace(actorResolver(tx)); name != "" {
			actor = name
		}
	}
	tx.Statement.SetColumn("updater", actor)
	return nil
}

// assignID 分配主键：ID 非零时保持不变（种子数据可显式指定）。
func assignID(tx *gorm.DB, id *int64) error {
	if id == nil {
		return ErrIDTargetNil
	}
	if *id != 0 {
		return nil
	}
	if idAssigner == nil {
		return ErrAssignerNotConfigured
	}
	return idAssigner.Assign(tx, id)
}

// 装配缺失类错误。
var (
	// ErrIDTargetNil 发号目标为空指针。
	ErrIDTargetNil = errBaseModel("model: ID 目标不能为空")
	// ErrAssignerNotConfigured 未注入发号器却需要发号。
	ErrAssignerNotConfigured = errBaseModel("model: 发号器未装配（调用 Configure 注入）")
)

type baseModelError string

func (e baseModelError) Error() string { return string(e) }

func errBaseModel(s string) error { return baseModelError(s) }
