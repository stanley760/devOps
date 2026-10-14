package iam

import (
	"encoding/json"

	"github.com/stanley760/devOps/devops-system/internal/domain/shared"
)

// 用户域事件名（持久化与投影路由的键，改动需同步迁移历史数据）。
const (
	EventUserRolesAssigned   = "iam.user.roles_assigned"
	EventUserDeactivated     = "iam.user.deactivated"
	EventUserActivated       = "iam.user.activated"
	EventUserDeleted         = "iam.user.deleted"
	EventUserPasswordChanged = "iam.user.password_changed"
)

// UserRolesAssigned 用户角色分配变更 → 重建 casbin g 行。
//
// 投影：casbin.RebuildUserGrouping（先清后建，天然幂等）。
// 改造前为 service/user.go:195 的同步调用，失败返回
// "用户已更新，但角色授权刷新失败，请重试更新"。
type UserRolesAssigned struct {
	shared.BaseEvent
	UserID    int64
	TenantID  int64
	RoleIDs   []int64
	RoleCodes []string
}

// EventName 实现 Event。
func (e UserRolesAssigned) EventName() string { return EventUserRolesAssigned }

func (e UserRolesAssigned) aggregateInfo() (string, int64) { return "iam.user", e.UserID }

func (e UserRolesAssigned) eventTenantID() int64 { return e.TenantID }

// UserDeactivated 用户被停用 → 撤销该用户全部会话（踢下线）。
//
// 投影：auth.RevokeAllSessions。改造前为 service/user.go:202 的同步调用。
type UserDeactivated struct {
	shared.BaseEvent
	UserID   int64
	TenantID int64
}

// EventName 实现 Event。
func (e UserDeactivated) EventName() string { return EventUserDeactivated }

func (e UserDeactivated) aggregateInfo() (string, int64) { return "iam.user", e.UserID }

func (e UserDeactivated) eventTenantID() int64 { return e.TenantID }

// UserActivated 用户被启用（当前无需外部动作，保留以便后续通知/审计场景）。
type UserActivated struct {
	shared.BaseEvent
	UserID   int64
	TenantID int64
}

// EventName 实现 Event。
func (e UserActivated) EventName() string { return EventUserActivated }

func (e UserActivated) aggregateInfo() (string, int64) { return "iam.user", e.UserID }

func (e UserActivated) eventTenantID() int64 { return e.TenantID }

// UserDeleted 用户被软删 → 清理 casbin g 行。
//
// 投影：casbin.RemoveUserGrouping。改造前为 service/user.go:250 的同步调用，
// 失败返回 "用户已删除，但角色授权清理失败"。
type UserDeleted struct {
	shared.BaseEvent
	UserID   int64
	TenantID int64
}

// EventName 实现 Event。
func (e UserDeleted) EventName() string { return EventUserDeleted }

func (e UserDeleted) aggregateInfo() (string, int64) { return "iam.user", e.UserID }

func (e UserDeleted) eventTenantID() int64 { return e.TenantID }

// UserEventPayload 是用户域事件的持久化载荷（事件表 Payload 列）。
//
// 领域事件是纯值类型（含 BaseEvent.Time），可直接 JSON 往返；
// 这里统一封装反序列化入口，供后台重试投影任务使用。
type UserEventPayload struct{}

// Decode 按事件名还原事件实例。未知事件名返回 ok=false，由调用方跳过。
func (UserEventPayload) Decode(name string, raw []byte) (shared.Event, bool, error) {
	switch name {
	case EventUserRolesAssigned:
		var e UserRolesAssigned
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	case EventUserDeactivated:
		var e UserDeactivated
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	case EventUserActivated:
		var e UserActivated
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	case EventUserDeleted:
		var e UserDeleted
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	case EventUserPasswordChanged:
		var e UserPasswordChanged
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	default:
		return nil, false, nil
	}
}

// UserPasswordChanged 口令变更 → 强制重新登录（撤销全部会话）。
//
// 改造前为 service/user.go:226（重置密码）与 service/auth.go（改密）里的
// 同步调用 authM.RevokeAllSessions；现由投影器执行，
// 会话撤销失败不再让「改密成功但用户仍在线」这一不一致状态静默发生。
type UserPasswordChanged struct {
	shared.BaseEvent
	UserID   int64
	TenantID int64
	// SelfChange 是否为本人改密（本人改密亦应重置其他会话）。
	SelfChange bool
}

// EventName 实现 Event。
func (e UserPasswordChanged) EventName() string { return EventUserPasswordChanged }

func (e UserPasswordChanged) aggregateInfo() (string, int64) { return "iam.user", e.UserID }

func (e UserPasswordChanged) eventTenantID() int64 { return e.TenantID }
