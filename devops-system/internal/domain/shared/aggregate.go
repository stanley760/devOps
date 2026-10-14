// Package shared 是领域层的共享内核：聚合基类、领域事件、领域错误与仓储端口约定。
//
// 分层约束（DDD 依赖方向）：
//
//	api/v1 → application → domain ← infrastructure
//	                ↘ infrastructure/persistence（实现 domain 的端口）
//
// 本包及整个 domain 树禁止 import GORM、huma、Redis、HTTP 等任何基础设施包，
// 只允许依赖 devops-infra/errs 与标准库。编译期即强制该约束成立。
package shared

import (
	"context"
	"time"
)

// AggregateRoot 聚合根基类。
//
// 聚合是 DDD 的一致性边界：聚合内部的所有变更必须在单一事务内完成，
// 跨聚合的一致性由领域事件（见 Event）而非分布式事务保证。
//
// 嵌入本类型的实体持有自己的领域事件缓冲，行为方法只负责在内存中记录事件，
// 由仓储在持久化时取出并发布——这样「业务状态变更」与「事件发布」天然同事务。
type AggregateRoot struct {
	events []Event
}

// RecordEvent 记录领域事件（由实体行为方法在状态变更后调用）。
func (a *AggregateRoot) RecordEvent(e Event) { a.events = append(a.events, e) }

// PullEvents 取出并清空事件缓冲。仓储在事务提交路径上调用。
func (a *AggregateRoot) PullEvents() []Event {
	if len(a.events) == 0 {
		return nil
	}
	out := make([]Event, len(a.events))
	copy(out, a.events)
	a.events = nil
	return out
}

// Events 返回当前待发布事件的只读副本（供测试断言用，不消费缓冲）。
func (a *AggregateRoot) Events() []Event {
	if len(a.events) == 0 {
		return nil
	}
	out := make([]Event, len(a.events))
	copy(out, a.events)
	return out
}

// Version 乐观并发控制版本号。仅在仓储需要乐观锁时使用；
// 当前实现用租户插件 + 唯一索引保证一致性，此字段为演进预留。
type Version int64

// AggregateID 聚合根标识别名（int64 主键，JSON 下发时由 API 层转字符串）。
type AggregateID = int64

// AuditInfo 审计留痕信息（值对象）。
//
// 对应既有表的 creator/updater 列。抽取为值对象后，领域层不依赖
// devops-common/auditctx 的具体上下文实现，只接受已解析好的操作者。
type AuditInfo struct {
	Creator string
	Updater string
}

// Actor 当前操作者（从认证上下文解析出的领域输入）。
type Actor struct {
	UserID   int64
	Username string
	TenantID int64
}

// NowFunc 时钟抽象。领域层不直接调用 time.Now()，便于测试注入确定性时钟。
type NowFunc func() time.Time

// SystemNow 默认时钟实现。
func SystemNow() time.Time { return time.Now() }

// PageParam 说明：分页入参复用 devops-common/domain.PageParam。
//
// 该类型已在共享内核中定义且**不依赖 GORM**（仅标准库 + 结构 tag），
// 同时被 gormx.FindPage 与 API 契约共用，无需在领域层重复定义。
// 各仓储 Filter 直接内嵌 domain.PageParam。
//
// 补充说明：devops-common/domain.PageParam 携带 query/form/doc tag，
// 这些 tag 只服务于 API 层反射（huma 生成 OpenAPI）与请求绑定，
// 领域层与仓储层只是**携带**该类型而不解析其 tag，
// 因此不构成 dao → API 契约的方向性依赖（真正的依赖方向由包 import 决定，
// 而非结构 tag）。

// TxManager 事务边界端口。
//
// 现状痛点：既有 dao.WithTx 把 *gorm.DB 完全封在 dao 包内，
// service 无法编排跨聚合用例的事务，被迫用手工补偿（如租户创建失败后
// 手工 HardDeleteTenant 回滚）。抽出本端口后，应用层可以声明事务边界，
// 仓储实现内部从 ctx 取事务句柄，从而在不泄漏 *gorm.DB 的前提下跨仓储编排。
type TxManager interface {
	// WithinTx 在单一事务内执行 fn。fn 返回错误则回滚。
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
