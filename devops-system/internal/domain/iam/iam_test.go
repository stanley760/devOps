package iam

import (
	"errors"
	"testing"
	"time"

	"github.com/stanley760/devOps/devops-infra/errs"
)

func fixedTime() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }

// bizCode 提取业务错误码（errs.BizError）。
func bizCode(err error) int {
	var be *errs.BizError
	if errors.As(err, &be) {
		return be.Code
	}
	return -1
}

func newUser(id int64, status UserStatus) *User {
	return &User{ID: id, TenantID: 7, Username: "alice", Status: status}
}

// TestUserEnsureDeletable 覆盖聚合内的删除不变式。
// 这三条规则改造前是 service/user.go:232-237 的裸 if，无法脱离 DB 验证。
func TestUserEnsureDeletable(t *testing.T) {
	cases := []struct {
		name      string
		userID    int64
		actorID   int64
		wantCode  int
		wantEmpty bool
	}{
		{name: "删除其他用户", userID: 42, actorID: 7, wantEmpty: true},
		{name: "删除自己被拒", userID: 7, actorID: 7, wantCode: errs.CodeParam},
		{name: "内置超管不可删", userID: builtinAdminID, actorID: 7, wantCode: errs.CodeParam},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := newUser(c.userID, UserStatusEnabled).EnsureDeletable(c.actorID)
			if c.wantEmpty {
				if err != nil {
					t.Fatalf("期望通过，实际报错: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("期望拒绝，实际通过")
			}
			if got := bizCode(err); got != c.wantCode {
				t.Fatalf("错误码 = %d, want %d", got, c.wantCode)
			}
		})
	}
}

// TestUserChangeStatusRecordsEvent 停用/启用应记录对应领域事件。
// 改造前该规则在 service/user.go:201-203，测试必须起 Redis + MySQL + HTTP 全栈。
func TestUserChangeStatusRecordsEvent(t *testing.T) {
	cases := []struct {
		name      string
		from      UserStatus
		to        UserStatus
		wantEvent string
	}{
		{name: "正常转停用", from: UserStatusEnabled, to: UserStatusDisabled, wantEvent: EventUserDeactivated},
		{name: "停用转正常", from: UserStatusDisabled, to: UserStatusEnabled, wantEvent: EventUserActivated},
		{name: "状态不变不记事件", from: UserStatusEnabled, to: UserStatusEnabled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUser(42, c.from)
			u.ChangeStatus(c.to, fixedTime())
			if u.Status != c.to {
				t.Fatalf("状态 = %d, want %d", u.Status, c.to)
			}
			events := u.Events()
			if c.wantEvent == "" {
				if len(events) != 0 {
					t.Fatalf("期望无事件，实际 %d 个", len(events))
				}
				return
			}
			if len(events) != 1 || events[0].EventName() != c.wantEvent {
				t.Fatalf("事件 = %v, want %s", events, c.wantEvent)
			}
		})
	}
}

// TestUserAssignRolesRecordsEvent 角色分配应记录 g 行同步事件。
func TestUserAssignRolesRecordsEvent(t *testing.T) {
	u := newUser(42, UserStatusEnabled)
	u.AssignRoles([]int64{3, 5}, []string{"editor", "ops"}, fixedTime())

	if len(u.RoleIDs) != 2 {
		t.Fatalf("RoleIDs = %v, want [3 5]", u.RoleIDs)
	}
	events := u.Events()
	if len(events) != 1 {
		t.Fatalf("事件数 = %d, want 1", len(events))
	}
	e, ok := events[0].(UserRolesAssigned)
	if !ok {
		t.Fatalf("事件类型 = %T, want UserRolesAssigned", events[0])
	}
	if len(e.RoleCodes) != 2 || e.RoleCodes[0] != "editor" {
		t.Fatalf("RoleCodes = %v, want [editor ops]", e.RoleCodes)
	}
}

// TestUserPullEventsConsumesBuffer PullEvents 应消费事件缓冲（只投递一次）。
func TestUserPullEventsConsumesBuffer(t *testing.T) {
	u := newUser(42, UserStatusEnabled)
	u.AssignRoles([]int64{1}, []string{"r"}, fixedTime())

	if got := u.PullEvents(); len(got) != 1 {
		t.Fatalf("首次 PullEvents = %d, want 1", len(got))
	}
	if got := u.PullEvents(); len(got) != 0 {
		t.Fatalf("二次 PullEvents = %d, want 0（应已消费）", len(got))
	}
	// Events 只读副本不影响缓冲。
	if got := u.Events(); len(got) != 0 {
		t.Fatalf("Events = %d, want 0", len(got))
	}
}

// TestRoleEnsureDeletable 内置角色不可删除。
func TestRoleEnsureDeletable(t *testing.T) {
	builtin := &Role{ID: 2, Code: "super_admin", Type: RoleTypeBuiltin}
	if err := builtin.EnsureDeletable(); err == nil {
		t.Fatal("内置角色应拒绝删除")
	} else if bizCode(err) != errs.CodeParam {
		t.Fatalf("错误码 = %d, want CodeParam", bizCode(err))
	}

	custom := &Role{ID: 9, Code: "editor", Type: RoleTypeCustom}
	if err := custom.EnsureDeletable(); err != nil {
		t.Fatalf("自定义角色应允许删除: %v", err)
	}
}

// TestRoleAssignMenusSkipsBuiltin 内置角色分配菜单不应派生 p 行事件。
// 这是防超管被锁死的关键规则（改造前 service/role.go:159）。
func TestRoleAssignMenusSkipsBuiltin(t *testing.T) {
	builtin := &Role{ID: 2, Code: "super_admin", Type: RoleTypeBuiltin}
	builtin.AssignMenus([]int64{1, 2}, []string{"system:user:list"}, fixedTime())
	if len(builtin.Events()) != 0 {
		t.Fatalf("内置角色不应产生 p 行事件，实际 %d 个", len(builtin.Events()))
	}

	custom := &Role{ID: 9, Code: "editor", Type: RoleTypeCustom}
	custom.AssignMenus([]int64{1}, []string{"system:user:list"}, fixedTime())
	events := custom.Events()
	if len(events) != 1 || events[0].EventName() != EventRoleMenusAssigned {
		t.Fatalf("事件 = %v, want RoleMenusAssigned", events)
	}
}

// TestValidateDataScope 自定义口径必须携带部门集合。
func TestValidateDataScope(t *testing.T) {
	cases := []struct {
		name       string
		scope      int8
		deptIDs    string
		wantReject bool
	}{
		{name: "全部权限无需部门", scope: 1, deptIDs: ""},
		{name: "自定义带部门", scope: 2, deptIDs: "[4,5]"},
		{name: "自定义缺部门被拒", scope: 2, deptIDs: "", wantReject: true},
		{name: "自定义空数组被拒", scope: 2, deptIDs: "[]", wantReject: true},
		{name: "自定义脏数据被拒", scope: 2, deptIDs: `["4"]`, wantReject: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateDataScope(c.scope, c.deptIDs)
			if c.wantReject && err == nil {
				t.Fatal("期望拒绝，实际通过")
			}
			if !c.wantReject && err != nil {
				t.Fatalf("期望通过，实际: %v", err)
			}
		})
	}
}

// TestTenantIsEnabled 租户三态可用性（正常/停用/过期）。
func TestTenantIsEnabled(t *testing.T) {
	now := fixedTime()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name       string
		status     int8
		expireTime *time.Time
		want       bool
	}{
		{name: "正常且永不过期", status: TenantStatusEnabled, want: true},
		{name: "停用", status: TenantStatusDisabled, want: false},
		{name: "已过期", status: TenantStatusEnabled, expireTime: &past, want: false},
		{name: "未过期", status: TenantStatusEnabled, expireTime: &future, want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tn := &Tenant{Status: c.status, ExpireTime: c.expireTime}
			if got := tn.IsEnabled(now); got != c.want {
				t.Fatalf("IsEnabled = %v, want %v", got, c.want)
			}
		})
	}
}

// TestTenantChangeRecordsDisableEvent 停用租户应记录全员下线事件。
func TestTenantChangeRecordsDisableEvent(t *testing.T) {
	tn := &Tenant{ID: 7, Status: TenantStatusEnabled}
	tn.Change(TenantUpdate{Name: "t7", Status: TenantStatusDisabled}, fixedTime())

	events := tn.Events()
	if len(events) != 1 || events[0].EventName() != EventTenantDisabled {
		t.Fatalf("事件 = %v, want TenantDisabled", events)
	}
}

// TestMenuEnsureNotSelfOrDescendant 防环：菜单不可挂到自己的后代下。
func TestMenuEnsureNotSelfOrDescendant(t *testing.T) {
	all := []*Menu{
		{ID: 1, ParentID: 0},
		{ID: 2, ParentID: 1},
		{ID: 3, ParentID: 2},
	}
	m := &Menu{ID: 2, ParentID: 1}

	if err := m.EnsureNotSelfOrDescendant(3, all); err == nil {
		t.Fatal("挂到自身后代应被拒绝")
	}
	if err := m.EnsureNotSelfOrDescendant(1, all); err != nil {
		t.Fatalf("挂到父级应允许: %v", err)
	}
	if err := m.EnsureNotSelfOrDescendant(0, all); err != nil {
		t.Fatalf("顶级应允许: %v", err)
	}
}
