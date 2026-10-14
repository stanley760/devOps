// Package command 是 CQRS 写侧：命令定义与处理器。
//
// 写侧统一走「加载聚合 → 调用聚合行为 → 仓储保存（含事件落库）」，
// 不直接触碰查询模型。事务边界由 shared.TxManager 显式声明。
//
// 相比改造前 service 层的三个关键改进：
//  1. 事务边界上移到应用层，跨聚合用例可原子化（原先只能手工补偿）；
//  2. 业务规则在聚合内（充血模型），处理器只做编排，不再写 if 判断业务；
//  3. casbin 授权同步改由领域事件驱动，失败不影响主流程响应。
package command

import (
	"context"

	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/domain/iam"
	"github.com/stanley760/devOps/devops-system/internal/domain/shared"
)

// errParam 构造参数错误（400）。
func errParam(msg string) error { return errs.New(errs.CodeParam, msg) }

// hashPassword 计算口令哈希。
//
// 注入点：生产用 bcrypt，测试可传替身，从而在无数据库的情况下
// 单测命令处理器的完整流程。
func hashPassword(plain string, hasher func(string) (string, error)) (string, error) {
	if hasher == nil {
		return "", errs.New(errs.CodeInternal, "口令哈希函数未装配")
	}
	h, err := hasher(plain)
	if err != nil {
		return "", errs.Wrap(err, errs.CodeInternal, "密码加密失败")
	}
	return h, nil
}

// UserCommand 用户写侧命令处理器。
//
// 依赖全部为端口（接口），故可注入内存假实现做单元测试——
// 这正是改造前 service 层零测试覆盖的结构性原因：dao 是包级函数，
// 无法注入替身。
type UserCommand struct {
	users iam.UserRepository
	txs   shared.TxManager
	now   shared.NowFunc
}

// NewUserCommand 构造用户命令处理器。
func NewUserCommand(users iam.UserRepository, txs shared.TxManager) *UserCommand {
	return &UserCommand{users: users, txs: txs, now: shared.SystemNow}
}

// CreateUserCmd 创建用户命令。
type CreateUserCmd struct {
	// TenantID 目标租户（由 claims 解析，handler 层传入）。
	TenantID int64
	Username string
	// Password 明文口令；处理器负责加密，领域层只见摘要。
	Password string
	Nickname string
	Remark   string
	DeptID   int64
	PostIDs  []int64
	Email    string
	Mobile   *string
	Gender   int8
	Avatar   string
	Status   int8
	RoleIDs  []int64
	// HashPassword 口令哈希函数（注入便于测试，缺省用 bcrypt）。
	HashPassword func(string) (string, error)
}

// CreateUser 创建用户。
//
// 一致性：用户行 + 角色关联 + 领域事件在单一事务内提交。
// 授权（casbin g 行）由 UserRolesAssigned 事件的投影器完成，不在本事务内，
// 因此不再出现「用户已创建但授权失败，请重试」的语义模糊错误。
func (c *UserCommand) Create(ctx context.Context, cmd CreateUserCmd) error {
	hash, err := hashPassword(cmd.Password, cmd.HashPassword)
	if err != nil {
		return err
	}

	// 唯一性预检：快路径拦截，真正的唯一性由数据库唯一索引兜底
	//（并发下两个请求可能同时通过预检，靠 uk_tenant_username 保证不重复）。
	exists, err := c.users.UsernameExists(ctx, cmd.Username, 0)
	if err != nil {
		return err
	}
	if exists {
		return errParam("用户名已存在")
	}
	if cmd.Mobile != nil && *cmd.Mobile != "" {
		exists, err = c.users.MobileExists(ctx, *cmd.Mobile, 0)
		if err != nil {
			return err
		}
		if exists {
			return errParam("手机号已存在")
		}
	}

	codes, err := c.users.RoleCodesByIDs(ctx, cmd.RoleIDs)
	if err != nil {
		return err
	}

	now := c.now()
	u := &iam.User{
		TenantID: cmd.TenantID,
		Username: cmd.Username,
		Password: hash,
		Nickname: cmd.Nickname,
		Remark:   cmd.Remark,
		DeptID:   cmd.DeptID,
		PostIDs:  cmd.PostIDs,
		Email:    cmd.Email,
		Mobile:   cmd.Mobile,
		Gender:   cmd.Gender,
		Avatar:   cmd.Avatar,
		Status:   iam.UserStatus(cmd.Status),
	}
	if err := u.ValidateForCreate(); err != nil {
		return err
	}
	u.AssignRoles(cmd.RoleIDs, codes, now)

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.users.Save(ctx, u)
	})
}

// UpdateUserCmd 更新用户命令。
type UpdateUserCmd struct {
	TenantID     int64
	UserID       int64
	Username     string
	Nickname     string
	Remark       string
	DeptID       int64
	PostIDs      []int64
	Email        string
	Mobile       *string
	Gender       int8
	Avatar       string
	Status       int8
	RoleIDs      []int64
	ActorID      int64
	HashPassword func(string) (string, error)
}

// UpdateUser 更新用户（资料 + 角色 + 状态）。
//
// 规则均由聚合承担：角色变更记录事件（触发 g 行同步与数据权限缓存失效），
// 状态 1→0 记录 UserDeactivated 事件（触发踢下线）。
func (c *UserCommand) Update(ctx context.Context, cmd UpdateUserCmd) error {
	u, err := c.users.Get(ctx, cmd.UserID)
	if err != nil {
		return err
	}

	// 唯一性：仅在值变化时校验（避免无谓查询）。
	if u.Username != cmd.Username {
		exists, err := c.users.UsernameExists(ctx, cmd.Username, u.ID)
		if err != nil {
			return err
		}
		if exists {
			return errParam("用户名已存在")
		}
	}
	if cmd.Mobile != nil && *cmd.Mobile != "" && (u.Mobile == nil || *u.Mobile != *cmd.Mobile) {
		exists, err := c.users.MobileExists(ctx, *cmd.Mobile, u.ID)
		if err != nil {
			return err
		}
		if exists {
			return errParam("手机号已存在")
		}
	}

	codes, err := c.users.RoleCodesByIDs(ctx, cmd.RoleIDs)
	if err != nil {
		return err
	}

	now := c.now()
	u.Username = cmd.Username
	u.ChangeProfile(iam.ProfileUpdate{
		Nickname: cmd.Nickname,
		Email:    cmd.Email,
		Remark:   cmd.Remark,
		DeptID:   cmd.DeptID,
		Gender:   cmd.Gender,
		Avatar:   cmd.Avatar,
		Mobile:   cmd.Mobile,
		PostIDs:  cmd.PostIDs,
	})
	u.AssignRoles(cmd.RoleIDs, codes, now)
	u.ChangeStatus(iam.UserStatus(cmd.Status), now)

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.users.Save(ctx, u)
	})
}

// ResetPasswordCmd 重置密码命令。
type ResetPasswordCmd struct {
	TenantID     int64
	UserID       int64
	Password     string
	HashPassword func(string) (string, error)
}

// ResetPassword 重置密码。
//
// 重置后需强制重新登录——由 UserPasswordChanged 事件触发全会话撤销，
// 领域层不直接调用 auth（保持领域层零基础设施依赖）。
func (c *UserCommand) ResetPassword(ctx context.Context, cmd ResetPasswordCmd) error {
	u, err := c.users.Get(ctx, cmd.UserID)
	if err != nil {
		return err
	}
	if err := u.EnsurePasswordResettable(); err != nil {
		return err
	}
	hash, err := hashPassword(cmd.Password, cmd.HashPassword)
	if err != nil {
		return err
	}
	u.ChangePassword(hash, c.now())

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.users.Save(ctx, u)
	})
}

// DeleteUserCmd 删除用户命令。
type DeleteUserCmd struct {
	TenantID int64
	UserID   int64
	// ActorID 操作者；用于「不能删除自己」的领域校验。
	ActorID int64
}

// DeleteUser 软删用户。
func (c *UserCommand) Delete(ctx context.Context, cmd DeleteUserCmd) error {
	u, err := c.users.Get(ctx, cmd.UserID)
	if err != nil {
		return err
	}
	if err := u.EnsureDeletable(cmd.ActorID); err != nil {
		return err
	}
	now := c.now()
	u.MarkDeleted(now)

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.users.Delete(ctx, u, u.Username)
	})
}

// UpdateProfileCmd 修改本人资料命令。
type UpdateProfileCmd struct {
	TenantID int64
	UserID   int64
	Nickname string
	Email    string
	Mobile   *string
	Gender   int8
	Avatar   string
}

// UpdateProfile 修改本人资料（不改角色与状态）。
func (c *UserCommand) UpdateProfile(ctx context.Context, cmd UpdateProfileCmd) error {
	u, err := c.users.Get(ctx, cmd.UserID)
	if err != nil {
		return err
	}
	u.ChangeProfile(iam.ProfileUpdate{
		Nickname: cmd.Nickname,
		Email:    cmd.Email,
		Gender:   cmd.Gender,
		Avatar:   cmd.Avatar,
		Mobile:   cmd.Mobile,
	})
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.users.Save(ctx, u)
	})
}
