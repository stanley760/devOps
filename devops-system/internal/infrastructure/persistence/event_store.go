package persistence

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-system/internal/domain/shared"
	"github.com/stanley760/devOps/devops-system/internal/model"
)

// 领域事件表名。
const eventTable = "t_system_domain_event"

// DomainEventPO 领域事件持久化对象。
//
// 新增表，不影响既有业务表与存量数据。表结构由 dbinit 的 SQL 迁移创建
// （幂等：表存在即跳过）。
//
// 复用 model.BaseModel 而非 domain.BaseModel：后者不存在于
// devops-common/domain 包（该包只有分页值对象）。
// 直接复用既有 model.BaseModel 可让事件表与业务表共享同一套
// 主键发号（BeforeCreate hook）与审计字段填充逻辑。
type DomainEventPO struct {
	model.BaseModel
	// EventName 事件名（投影路由键）。
	EventName string `gorm:"size:64;not null;index"`
	// AggregateType 聚合类型（如 iam.user）。
	AggregateType string `gorm:"size:32;not null;index"`
	// AggregateID 聚合 ID。
	AggregateID int64 `gorm:"not null;index"`
	// TenantID 租户 ID（投影时按租户定位 casbin 域）。
	TenantID int64 `gorm:"index;not null"`
	// Payload 事件载荷 JSON。
	Payload string `gorm:"type:json"`
	// OccurredAt 事件发生时间。
	OccurredAt time.Time `gorm:"not null"`
	// ProjectedAt 投影完成时间；NULL 表示待投影。
	ProjectedAt *time.Time `gorm:"index"`
	// Attempts 投影尝试次数（失败重试计数）。
	Attempts int `gorm:"not null;default:0"`
	// LastError 最近一次投影错误。
	LastError string `gorm:"size:500"`
}

// TableName 表名。
func (DomainEventPO) TableName() string { return eventTable }

// eventStore 是 shared.EventStore 的 GORM 实现。
type eventStore struct{}

// NewEventStore 构造事件存储。
func NewEventStore() shared.EventStore { return eventStore{} }

// Append 在当前事务上下文中追加领域事件。
func (eventStore) Append(ctx context.Context, events ...shared.Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]DomainEventPO, 0, len(events))
	for _, e := range events {
		payload, err := json.Marshal(e)
		if err != nil {
			return err
		}
		at, aggType, aggID := eventMeta(e)
		rows = append(rows, DomainEventPO{
			EventName:     e.EventName(),
			AggregateType: aggType,
			AggregateID:   aggID,
			TenantID:      eventTenant(e),
			Payload:       string(payload),
			OccurredAt:    at,
		})
	}
	if err := db(ctx).Create(&rows).Error; err != nil {
		return err
	}
	return nil
}

// appendEvents 仓储内部的便捷调用。
func appendEvents(ctx context.Context, events ...shared.Event) error {
	if len(events) == 0 {
		return nil
	}
	return eventStore{}.Append(ctx, events...)
}

// eventMeta 从事件提取聚合标识与发生时间。
func eventMeta(e shared.Event) (time.Time, string, int64) {
	type aggInfo interface {
		aggregateInfo() (string, int64)
	}
	if a, ok := e.(aggInfo); ok {
		t, id := a.aggregateInfo()
		return e.OccurredAt(), t, id
	}
	return e.OccurredAt(), "", 0
}

// eventTenant 从事件提取租户 ID（投影时需要 casbin 域）。
func eventTenant(e shared.Event) int64 {
	type tenantInfo interface{ eventTenantID() int64 }
	if t, ok := e.(tenantInfo); ok {
		return t.eventTenantID()
	}
	return 0
}

// dispatcher 是领域事件分发器：按事件名路由到对应投影器。
//
// 语义要点（这是替代手工补偿的关键）：
//   - 投影失败**不返回错误给主流程**，业务写已提交，不应因授权投影失败而让用户困惑
//   - 失败时事件保持 projected_at = NULL 并累加 attempts，由后台任务重试
//   - 每个投影器实现必须幂等（casbin 的 RebuildXxx 系列先清后建，天然幂等）
type dispatcher struct {
	mu         sync.RWMutex
	projectors map[string][]shared.EventProjector
}

// NewDispatcher 构造分发器并注册投影器。
func NewDispatcher(projectors ...shared.EventProjector) shared.EventPublisher {
	d := &dispatcher{projectors: make(map[string][]shared.EventProjector)}
	for _, p := range projectors {
		if p == nil {
			continue
		}
		for _, name := range p.Handles() {
			d.projectors[name] = append(d.projectors[name], p)
		}
	}
	return d
}

// Publish 分发事件。投影失败仅记录，不向上返回。
func (d *dispatcher) Publish(ctx context.Context, events ...shared.Event) error {
	for _, e := range events {
		d.mu.RLock()
		targets := d.projectors[e.EventName()]
		d.mu.RUnlock()

		for _, p := range targets {
			if err := p.Handle(ctx, e); err != nil {
				// 投影失败不回传主流程：事件已持久化，可由重试任务补偿。
				slog.Error("领域事件投影失败",
					"event", e.EventName(),
					"projector", projectorName(p),
					"at", e.OccurredAt(),
					"err", err)
			}
		}
	}
	return nil
}

func projectorName(p shared.EventProjector) string {
	if n, ok := p.(interface{ Name() string }); ok {
		return n.Name()
	}
	return "unknown"
}

// PendingEvents 取出待投影事件（供后台重试任务调用）。
func PendingEvents(ctx context.Context, limit int) ([]DomainEventPO, error) {
	var rows []DomainEventPO
	err := db(ctx).Where("projected_at IS NULL").
		Order("occurred_at ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// MarkProjected 标记事件投影完成。
func MarkProjected(ctx context.Context, id int64) error {
	now := time.Now()
	return db(ctx).Model(&DomainEventPO{}).
		Where("id = ?", id).Updates(map[string]any{
		"projected_at": &now,
		"last_error":   "",
	}).Error
}

// MarkProjectFailed 记录投影失败并累加重试次数。
func MarkProjectFailed(ctx context.Context, id int64, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	return db(ctx).Model(&DomainEventPO{}).Where("id = ?", id).Updates(map[string]any{
		"attempts":   gorm.Expr("attempts + 1"),
		"last_error": truncate(msg, 500),
	}).Error
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
