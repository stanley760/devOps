package iam

import (
	"encoding/json"

	"github.com/stanley760/devops/devops-system/internal/domain/shared"
)

// 角色域事件名。
const (
	EventRoleMenusAssigned = "iam.role.menus_assigned"
	EventRoleDeleted       = "iam.role.deleted"
)

// RoleMenusAssigned 角色菜单分配变更 → 按 (role, tenant) 快照重建 casbin p 行。
//
// 投影：casbin.RebuildRolePolicies（先清后建，幂等）。
// 改造前为 service/role.go:168 的同步调用，失败返回
// "菜单已保存，但权限策略刷新失败，请重试"。内置角色不产生此事件。
type RoleMenusAssigned struct {
	shared.BaseEvent
	RoleID      int64
	RoleCode    string
	TenantID    int64
	Permissions []string
}

// EventName 实现 Event。
func (e RoleMenusAssigned) EventName() string { return EventRoleMenusAssigned }

func (e RoleMenusAssigned) aggregateInfo() (string, int64) { return "iam.role", e.RoleID }

func (e RoleMenusAssigned) eventTenantID() int64 { return e.TenantID }

// RoleDeleted 角色软删 → 清理 p 行与 g 行引用。
//
// 投影：casbin.RemoveRolePolicies + RemoveRoleGrouping。
// 改造前为 service/role.go:188-193 的两次同步调用。
type RoleDeleted struct {
	shared.BaseEvent
	RoleID   int64
	RoleCode string
	TenantID int64
}

// EventName 实现 Event。
func (e RoleDeleted) EventName() string { return EventRoleDeleted }

func (e RoleDeleted) aggregateInfo() (string, int64) { return "iam.role", e.RoleID }

func (e RoleDeleted) eventTenantID() int64 { return e.TenantID }

// RoleEventPayload 是角色域事件的持久化载荷编解码入口。
type RoleEventPayload struct{}

// Decode 按事件名还原事件实例。未知事件名返回 ok=false。
func (RoleEventPayload) Decode(name string, raw []byte) (shared.Event, bool, error) {
	switch name {
	case EventRoleMenusAssigned:
		var e RoleMenusAssigned
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	case EventRoleDeleted:
		var e RoleDeleted
		err := json.Unmarshal(raw, &e)
		return e, err == nil, err
	default:
		return nil, false, nil
	}
}
