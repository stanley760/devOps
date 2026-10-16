// Package model 定义 devops-system 的全部表模型（t_tenant + t_system_*）。
//
// 基模型已上提到 devops-common/model（与 devops-user 共用同一份实现）：
//   - BaseModel        全局表内嵌（t_tenant、t_system_menu）
//   - BaseTenantModel  租户表内嵌 = BaseModel + tenant_id 判别列
//
// 本文件只保留**本服务专属**的部分：租户判别列、发号器桥接、
// 以及把 auditctx 的认证操作者接到共用实现上。
//
// 主键一律由 devops-id 发号：autoIncrement:false + BeforeCreate hook；
// 种子数据显式指定 ID（admin=1、部门=100 起、菜单 1-7 等），ID 为 0 时才自动发号。
package model

import (
	"strings"

	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-common/auditctx"
	commonmodel "github.com/stanley760/devops/devops-common/model"
	"github.com/stanley760/devops/devops-id/pkg/idgorm"
)

// idHook 本服务的 idgorm 钩子：用于 BizTagOf（按表名推导 biz_tag）。
//
// 发号本身（BeforeCreate 分配主键）已由 commonmodel.BaseModel 负责，
// 但 idgorm.Hook 的 BizTag 推导能力仍需保留，故保留此实例。
var idHook = idgorm.New(nil, nil)

// actorFromAuditCtx 从 ctx 取认证用户名；无认证操作者时返回空串。
//
// 抽成函数是为了让 SetIDGenerator 与 ConfigureModel 走同一条装配路径——
// 否则只调 SetIDGenerator（漏配 ActorResolver）会导致 creator/updater 全部退化为
// system，审计字段静默失效，且不会有任何报错。
func actorFromAuditCtx(tx *gorm.DB) string {
	actor, ok := auditctx.From(tx.Statement.Context)
	if !ok {
		return ""
	}
	return strings.TrimSpace(actor.Username)
}

// AuditActorFromContext 从 ctx 取认证用户名；无认证操作者时返回空串。
//
// 导出供测试与特殊场景复用：B 端标准语义就是 auditctx里的登录用户名。
func AuditActorFromContext(tx *gorm.DB) string { return actorFromAuditCtx(tx) }

// ConfigureAuditActor 仅装配审计操作者解析（不动发号器）。
//
// 用于「不发号」的路径：单测用显式 ID 构造记录时只需审计钩子正确。
func ConfigureAuditActor(resolver commonmodel.ActorResolver) {
	commonmodel.Configure(commonmodel.CurrentIDAssigner(), resolver)
}

// SetIDGenerator 注入发号客户端（server 命令装配期调用；migrate 种子不依赖）。
//
// 同时配置到 commonmodel：共用 BaseModel 的 BeforeCreate 经 idHook（*idgorm.Hook，
// 满足 IDAssigner）分配主键。ActorResolver 一并装配为 auditctx 解析（B 端语义）。
func SetIDGenerator(generator idgorm.Generator) {
	idHook.SetGenerator(generator)
	commonmodel.Configure(idHook, actorFromAuditCtx)
}

// ConfigureModel 装配共用基模型：发号器 + 认证操作者解析。
//
// B 端语义：creator/updater 取当前登录用户名（auditctx 由中间件注入）。
// 无认证操作者时（种子数据、登录前的写操作）共用实现回退为 system。
func ConfigureModel(generator idgorm.Generator) {
	SetIDGenerator(generator)
}

// IDGenerator 返回当前发号客户端（未装配时为 nil）。
func IDGenerator() idgorm.Generator { return idHook.Generator() }

// BaseModel 共用全局表基模型（别名，指向 devops-common/model）。
type BaseModel = commonmodel.BaseModel

// BaseTenantModel 租户表基模型：共享数据库中的 tenant_id 判别列。
//
// ⚠️ 不放进 devops-common：C 端单租户，没有这个概念，
// 放上去等于让 common 携带只有 B 端需要的字段。
type BaseTenantModel struct {
	BaseModel
	TenantID int64 `gorm:"index;not null"`
}

// BizTagOf 模型 → biz_tag（独立 snowflake worker 空间标识）。
func BizTagOf(m any) (string, error) { return idHook.BizTagOf(m) }
