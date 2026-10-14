package iam

import (
	"strings"
	"time"

	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/domain/shared"
)

// Dept 部门聚合根（租户内）。
//
// 注意：t_system_dept 无 ancestors 列（对齐 yudao），树的父子关系仅由
// parent_id 表达，路径信息在查询侧按需组装。
type Dept struct {
	shared.AggregateRoot

	ID       int64
	TenantID int64
	ParentID int64
	Name     string
	Sort     int
	// LeaderUserID 负责人（用户 ID；列名 leader_user_id）。
	LeaderUserID int64
	Phone        string
	Email        string
	Status       int8
	CreateTime   time.Time
	UpdateTime   time.Time
}

// DeptUpdate 部门变更入参（领域输入）。
type DeptUpdate struct {
	ParentID     int64
	Name         string
	Sort         int
	LeaderUserID int64
	Phone        string
	Email        string
	Status       int8
}

// ValidateForCreate 创建前校验。
func (d *Dept) ValidateForCreate() error {
	if strings.TrimSpace(d.Name) == "" {
		return errs.New(errs.CodeParam, "部门名称不能为空")
	}
	return nil
}

// Change 应用变更。
func (d *Dept) Change(up DeptUpdate) {
	d.ParentID = up.ParentID
	d.Name = up.Name
	d.Sort = up.Sort
	d.LeaderUserID = up.LeaderUserID
	d.Phone = up.Phone
	d.Email = up.Email
	d.Status = up.Status
}

// EnsureNotSelf 校验不可把自己设为上级。
func (d *Dept) EnsureNotSelf(parentID int64) error {
	if parentID == d.ID {
		return errs.New(errs.CodeParam, "上级部门不能是自己")
	}
	return nil
}

// HasChildrenReport 部门下是否仍有子部门或成员（删除前置校验）。
//
// 返回的提示文案与既有实现保持一致，避免前端提示变化。
func HasChildrenReport(hasChildren bool, hasMembers bool) error {
	if hasChildren {
		return errs.New(errs.CodeParam, "存在下级部门，不能删除")
	}
	if hasMembers {
		return errs.New(errs.CodeParam, "部门下存在用户，不能删除")
	}
	return nil
}

// Post 岗位聚合根（租户内）。
type Post struct {
	shared.AggregateRoot

	ID         int64
	TenantID   int64
	Code       string // 租户内唯一
	Name       string
	Sort       int
	Status     int8
	Remark     string
	CreateTime time.Time
	UpdateTime time.Time
}

// PostUpdate 岗位变更入参。
type PostUpdate struct {
	Code   string
	Name   string
	Sort   int
	Status int8
	Remark string
}

// ValidateForCreate 创建前校验：编码非空。
func (p *Post) ValidateForCreate() error {
	if strings.TrimSpace(p.Code) == "" {
		return errs.New(errs.CodeParam, "岗位编码不能为空")
	}
	return nil
}

// Change 应用变更。
func (p *Post) Change(up PostUpdate) {
	p.Code = up.Code
	p.Name = up.Name
	p.Sort = up.Sort
	p.Status = up.Status
	p.Remark = up.Remark
}

// Tenant 租户聚合根（全局表，无 tenant_id —— 租户注册表本身）。
//
// 租户状态与用户/角色状态联动：停用租户 → 全员会话失效 + 拒绝登录。
// 改造前该联动散落在 service/tenant.go（RevokeTenantSessions 调用），
// 现以领域事件表达。
type Tenant struct {
	shared.AggregateRoot

	ID   int64
	Name string
	// Code 全局唯一（uk_code），前端租户标识与登录入参。
	Code          string
	ContactName   string
	ContactMobile string
	Status        int8
	// ExpireTime nil 表示永不过期。
	ExpireTime *time.Time
	Remark     string
	CreateTime time.Time
	UpdateTime time.Time
}

// 租户状态。
const (
	TenantStatusDisabled int8 = 0
	TenantStatusEnabled  int8 = 1
)

// TenantUpdate 租户变更入参。
type TenantUpdate struct {
	Name          string
	ContactName   string
	ContactMobile string
	Status        int8
	ExpireTime    *time.Time
	Remark        string
}

// IsEnabled 租户是否可用（存在、未停用、未过期）。
func (t *Tenant) IsEnabled(now time.Time) bool {
	if t.Status != TenantStatusEnabled {
		return false
	}
	if t.ExpireTime != nil && t.ExpireTime.Before(now) {
		return false
	}
	return true
}

// EnsureUsable 租户可用性校验（登录与中间件共用同一判定口径）。
func (t *Tenant) EnsureUsable(now time.Time) error {
	if !t.IsEnabled(now) {
		return errs.New(errs.CodeForbidden, "租户不可用（停用或已过期）")
	}
	return nil
}

// Change 应用变更；状态转为停用时记录事件（全员踢下线）。
func (t *Tenant) Change(up TenantUpdate, at time.Time) {
	was := t.Status
	t.Name = up.Name
	t.ContactName = up.ContactName
	t.ContactMobile = up.ContactMobile
	t.Status = up.Status
	t.ExpireTime = up.ExpireTime
	t.Remark = up.Remark
	t.UpdateTime = at

	if was == TenantStatusEnabled && up.Status == TenantStatusDisabled {
		t.RecordEvent(TenantDisabled{
			BaseEvent: shared.BaseEvent{Time: at},
			TenantID:  t.ID,
		})
	}
}

// SyncStatusWithExpiry 按过期时间推导状态（未来→启用，过去→停用）。
//
// 业务规则：更新租户时若显式传了过期时间，状态应与之保持一致，
// 否则会出现「已过期但状态仍为启用」的矛盾态。
// 改造前该规则在 service/tenant.go:130-140 以 updates map 表达。
func (t *Tenant) SyncStatusWithExpiry(now time.Time) {
	if t.ExpireTime == nil {
		return // 永不过期：不改动状态
	}
	want := TenantStatusEnabled
	if !t.ExpireTime.After(now) {
		want = TenantStatusDisabled
	}
	if t.Status != want {
		t.Change(TenantUpdate{
			Name:          t.Name,
			ContactName:   t.ContactName,
			ContactMobile: t.ContactMobile,
			ExpireTime:    t.ExpireTime,
			Remark:        t.Remark,
			Status:        want,
		}, now)
	}
}

// EnsureNotDefaultDisable 禁止停用默认租户（避免把自己锁在门外）。
//
// 该规则在改造前不存在，属本次重构新增的显式业务约束。
func (t *Tenant) EnsureNotDefaultDisable(code string) error {
	if code == "default" && t.Status == TenantStatusDisabled {
		return errs.New(errs.CodeParam, "默认租户不可停用")
	}
	return nil
}
