package iam

import (
	"encoding/json"

	"github.com/stanley760/devops/devops-system/internal/domain/shared"
)

// 租户域事件名。
const (
	EventTenantDisabled    = "iam.tenant.disabled"
	EventTenantAdminSeeded = "iam.tenant.admin_seeded"
)

// TenantDisabled 租户被停用 → 撤销该租户全部会话（全员下线）。
//
// 投影：auth.RevokeTenantSessions。
// 改造前为 service/tenant.go:169/192 的同步调用，失败仅记日志；
// 现以事件表达，投影失败可重试且不丢。
type TenantDisabled struct {
	shared.BaseEvent
	TenantID int64
}

// EventName 实现 Event。
func (e TenantDisabled) EventName() string { return EventTenantDisabled }

func (e TenantDisabled) aggregateInfo() (string, int64) { return "iam.tenant", e.TenantID }

func (e TenantDisabled) eventTenantID() int64 { return e.TenantID }

// TenantAdminSeeded 新租户初始超管已创建 → 写入域内全通 p 行与 g 行绑定。
//
// 改造前为 service/tenant.go:110 的同步调用 authz.SeedTenantAdmin，
// 失败时返回 "租户已创建，但超管授权写入失败，请重试"——
// 业务数据已提交而授权未生效，客户端无从判断该重试还是人工介入。
// 现以事件表达：主流程返回成功，投影失败由重试任务补偿且不会丢失。
type TenantAdminSeeded struct {
	shared.BaseEvent
	TenantID    int64
	AdminUserID int64
}

// EventName 实现 Event。
func (e TenantAdminSeeded) EventName() string { return EventTenantAdminSeeded }

func (e TenantAdminSeeded) aggregateInfo() (string, int64) { return "iam.tenant", e.TenantID }

func (e TenantAdminSeeded) eventTenantID() int64 { return e.TenantID }

// TenantEventPayload 是租户域事件的持久化载荷编解码入口。
type TenantEventPayload struct{}

// Decode 按事件名还原事件实例。未知事件名返回 ok=false。
func (TenantEventPayload) Decode(name string, raw []byte) (shared.Event, bool, error) {
	switch name {
	case EventTenantDisabled:
		var e TenantDisabled
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	case EventTenantAdminSeeded:
		var e TenantAdminSeeded
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	default:
		return nil, false, nil
	}
}
