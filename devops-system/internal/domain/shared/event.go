package shared

import (
	"context"
	"time"
)

// Event 领域事件。
//
// 用途：把「聚合状态已变更」与「外部系统投影需同步」解耦。
//
// 改造前的痛点：casbin 策略同步是 service 里的同步调用，写失败时只能返回
// "用户已创建，但角色授权写入失败，请重试更新" 这类补偿式错误——业务行已提交、
// 授权未生效，客户端无法判断该重试还是该人工介入（3 处：user.Create /
// user.Update / role.AssignMenus，另加 tenant 的手工 HardDeleteTenant 补偿）。
//
// 改造后：聚合在事务内记录事件，仓储随业务写同事务落库，投影视图由
// 投影器消费。授权同步失败不再污染主流程响应，且因为事件已持久化，
// 失败可重试而不会丢——这正是 CQRS 中「写模型 / 读模型分离」的价值。
type Event interface {
	// EventName 事件名（用作持久化与投影路由的键）。
	EventName() string
	// OccurredAt 事件发生时间。
	OccurredAt() time.Time
}

// BaseEvent 事件基类型，嵌入后自动实现 Event 接口的 OccurredAt。
type BaseEvent struct {
	Time time.Time
}

// OccurredAt 实现 Event。
func (e BaseEvent) OccurredAt() time.Time { return e.Time }

// EventStore 事件存储端口（写侧）。
//
// 事件与业务数据在同一事务内写入，保证「状态变更」与「待同步事实」原子落库。
// 实现见 infrastructure/persistence/gorm_store。
type EventStore interface {
	// Append 在当前事务上下文中追加事件。
	Append(ctx context.Context, events ...Event) error
}

// EventPublisher 事件分发端口（基础设施侧实现）。
type EventPublisher interface {
	// Publish 分发事件给所有匹配的投影处理器。
	Publish(ctx context.Context, events ...Event) error
}

// EventProjector 投影器：订阅某类事件，把写模型变更投影到外部读模型（casbin）。
//
// 幂等性要求：投影器可能被重复调用（失败重试），故每个 Handle 实现
// 必须幂等——对 casbin 而言，RebuildXxx 系列函数本身即幂等（先清后建）。
type EventProjector interface {
	// Handles 返回本投影器关心的事件名。
	Handles() []string
	// Handle 处理单个事件。返回错误由分发器按策略重试，不影响主流程响应。
	Handle(ctx context.Context, e Event) error
}
