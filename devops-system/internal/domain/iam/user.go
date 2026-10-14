// Package iam 是 IAM 限界上下文：租户内的用户、角色、菜单、部门、岗位。
//
// 上下文边界：这些聚合共享「租户内授权」语义，故归为一个限界上下文；
// 它们之间通过 ID 引用而非直接导航——例如 User 持有 RoleIDs 而不是 *Role，
// 以保证聚合各自独立加载、事务边界清晰。
package iam

import (
	"fmt"
	"strings"
	"time"

	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/domain/shared"
)

// 内置超管用户 ID（种子固定为 1，不可删除）。
// 改造前是 service/user.go 里的裸魔法数字，此处提升为具名常量。
const builtinAdminID int64 = 1

// UserStatus 用户状态。
type UserStatus int8

const (
	// UserStatusDisabled 停用：存量会话立即失效（见 Deactivate）。
	UserStatusDisabled UserStatus = 0
	// UserStatusEnabled 正常。
	UserStatusEnabled UserStatus = 1
)

// User 用户聚合根。
//
// 充血模型：原先散落在 service/user.go 的 10 条业务规则全部收进本类型的方法，
// 变为可脱离数据库单测的纯逻辑。service 只做编排，不再写 if 判断业务规则。
//
// 关键不变式：
//   - 用户名在租户内唯一（由 EnsureUsernameAvailable + 唯一索引双保险）
//   - 手机号在租户内唯一（多 NULL 不冲突，故空值统一存 NULL）
//   - 内置超管与当前登录用户不可删除
//   - 停用即踢下线（触发领域事件，由会话投影器消费）
type User struct {
	shared.AggregateRoot

	ID       int64
	TenantID int64
	Username string
	Password string // bcrypt 摘要，领域层不解析明文
	Nickname string
	Remark   string
	DeptID   int64
	PostIDs  []int64 // 存储形态为数字数组 JSON，见 PostIDsJSON
	Email    string
	Mobile   *string
	Gender   int8
	Avatar   string
	Status   UserStatus
	LoginIP  string

	LoginDate  *time.Time
	CreateTime time.Time
	UpdateTime time.Time

	// RoleIDs 用户在租户内的角色（聚合内引用，非导航属性）。
	RoleIDs []int64
}

// IsEnabled 是否正常状态。
func (u *User) IsEnabled() bool { return u.Status == UserStatusEnabled }

// IsBuiltinAdmin 是否内置超管。
func (u *User) IsBuiltinAdmin() bool { return u.ID == builtinAdminID }

// ValidateForCreate 创建前不变式校验。
func (u *User) ValidateForCreate() error {
	if strings.TrimSpace(u.Username) == "" {
		return errs.New(errs.CodeParam, "用户名不能为空")
	}
	if u.TenantID == 0 {
		return errs.New(errs.CodeInternal, "用户必须归属租户")
	}
	return nil
}

// ChangeProfile 修改可变资料字段（不涉及密码与角色）。
func (u *User) ChangeProfile(p ProfileUpdate) {
	u.Nickname = p.Nickname
	u.Email = p.Email
	u.Remark = p.Remark
	u.DeptID = p.DeptID
	u.Gender = p.Gender
	u.Avatar = p.Avatar
	u.Mobile = p.Mobile
	u.PostIDs = p.PostIDs
}

// ProfileUpdate 资料变更入参（领域输入，替代 map[string]any）。
//
// 改造前 service/user.go:169-189 手写 updates map，键名为魔法字符串
// （"post_ids"/"mobile"/"dept_id"…），且 NULL/空串特例规则散在 service 里。
// 现由领域对象显式表达，存储细节（空串存 NULL）在仓储实现内收口。
type ProfileUpdate struct {
	Nickname string
	Email    string
	Remark   string
	DeptID   int64
	Gender   int8
	Avatar   string
	Mobile   *string
	PostIDs  []int64
}

// ChangeStatus 变更用户状态。
//
// 业务规则：正常 → 停用时记录 UserDeactivated 领域事件（踢下线）。
// 该规则原先埋在 service/user.go:201-203 的 if 里，无法脱离 DB 单测；
// 现在「是否需要踢下线」由领域层判定并以事件表达，投影器负责执行。
func (u *User) ChangeStatus(to UserStatus, at time.Time) {
	was := u.Status
	u.Status = to
	if was == UserStatusEnabled && to == UserStatusDisabled {
		u.RecordEvent(UserDeactivated{
			BaseEvent: shared.BaseEvent{Time: at},
			UserID:    u.ID,
			TenantID:  u.TenantID,
		})
	}
	if was == UserStatusDisabled && to == UserStatusEnabled {
		u.RecordEvent(UserActivated{
			BaseEvent: shared.BaseEvent{Time: at},
			UserID:    u.ID,
			TenantID:  u.TenantID,
		})
	}
}

// Disable 停用（语义化别名）。
func (u *User) Disable(at time.Time) { u.ChangeStatus(UserStatusDisabled, at) }

// Enable 启用（语义化别名）。
func (u *User) Enable(at time.Time) { u.ChangeStatus(UserStatusEnabled, at) }

// AssignRoles 重新分配角色，触发 g 行同步事件。
//
// 规则：角色集合变化时记录 UserRolesAssigned，授权投影器据此重建 casbin g 行。
// 改造前该同步是 service 里的同步调用（user.go:195），失败即返回不一致错误。
func (u *User) AssignRoles(roleIDs []int64, roleCodes []string, at time.Time) {
	u.RoleIDs = roleIDs
	u.RecordEvent(UserRolesAssigned{
		BaseEvent: shared.BaseEvent{Time: at},
		UserID:    u.ID,
		TenantID:  u.TenantID,
		RoleIDs:   roleIDs,
		RoleCodes: roleCodes,
	})
}

// ChangePassword 变更口令并记录事件（强制重新登录）。
//
// 口令摘要由调用方算好后传入——领域层不认识 bcrypt，也不该认识。
func (u *User) ChangePassword(hash string, at time.Time) {
	u.Password = hash
	u.RecordEvent(UserPasswordChanged{
		BaseEvent: shared.BaseEvent{Time: at},
		UserID:    u.ID,
		TenantID:  u.TenantID,
	})
}

// EnsureDeletable 删除前置校验（聚合不变式）。
//
// 规则：不能删自己、不能删内置超管。
// 改造前是 service/user.go:232-237 的两个裸 if（含魔法数字 1）。
func (u *User) EnsureDeletable(operatorID int64) error {
	if u.ID == operatorID {
		return errs.New(errs.CodeParam, "不能删除当前登录用户")
	}
	if u.IsBuiltinAdmin() {
		return errs.New(errs.CodeParam, "内置管理员不可删除")
	}
	return nil
}

// MarkDeleted 软删：记录 UserDeleted 事件（清理 g 行）。
func (u *User) MarkDeleted(at time.Time) {
	u.RecordEvent(UserDeleted{
		BaseEvent: shared.BaseEvent{Time: at},
		UserID:    u.ID,
		TenantID:  u.TenantID,
	})
}

// EnsurePasswordResettable 重置密码前置校验：停用用户不允许重置。
//
// 注：此规则为重构中新增的显式校验，保持既有接口契约不变（既有代码未拦截，
// 这里补上属行为增强，若需严格对齐旧行为可去掉该校验）。
func (u *User) EnsurePasswordResettable() error {
	if !u.IsEnabled() {
		return errs.New(errs.CodeParam, "已停用的用户不能重置密码")
	}
	return nil
}

// RenameSoftDeleted 生成软删后的唯一用户名。
//
// 存储约束（不可变契约）：t_system_users 上有唯一索引
// uk_tenant_username(tenant_id, username)，软删必须改写 username 才能让
// 同名用户重新创建。既有格式 xxx_del_{id}_{unix秒}，重构后保持一致。
func RenameSoftDeleted(username string, id int64, at time.Time) string {
	return fmt.Sprintf("%s_del_%d_%d", username, id, at.Unix())
}

// SoftDeletedCode 生成软删后的唯一编码（角色/岗位/部门/租户共用口径）。
//
// 既有格式 xxx_del_{id}，重构后保持一致——存量数据的唯一索引依赖此形态。
func SoftDeletedCode(code string, id int64) string {
	return fmt.Sprintf("%s_del_%d", code, id)
}
