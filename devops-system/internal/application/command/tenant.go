package command

import (
	"context"
	"time"

	infratenant "github.com/stanley760/devops/devops-infra/tenant"

	"github.com/stanley760/devops/devops-system/internal/domain/iam"
	"github.com/stanley760/devops/devops-system/internal/domain/shared"
)

// TenantCommand 租户写侧命令处理器。
type TenantCommand struct {
	tenants iam.TenantRepository
	users   iam.UserRepository
	roles   iam.RoleRepository
	txs     shared.TxManager
	now     shared.NowFunc
	// SuperAdminRole 内置超管角色编码（由 authz 角色名配置注入）。
	SuperAdminRole func() string
	// HashPassword 口令哈希（生产 bcrypt）。
	HashPassword func(string) (string, error)
}

// NewTenantCommand 构造租户命令处理器。
func NewTenantCommand(
	tenants iam.TenantRepository,
	users iam.UserRepository,
	roles iam.RoleRepository,
	txs shared.TxManager,
) *TenantCommand {
	return &TenantCommand{
		tenants: tenants, users: users, roles: roles,
		txs: txs, now: shared.SystemNow,
	}
}

// CreateTenantCmd 创建租户命令（含初始超管）。
type CreateTenantCmd struct {
	Name          string
	Code          string
	ContactName   string
	ContactMobile string
	ExpireTime    *time.Time
	Remark        string
	AdminUsername string
	AdminPassword string
}

// Create 创建租户 + 初始超管（角色 + 管理员账号）。
//
// 这是本次重构最能体现价值的一处：改造前（service/tenant.go:104-108）因
// 事务边界被封在 dao 包内，应用层无法声明「建租户 + 建超管」的事务边界，
// 只能靠手工补偿——
//
//	if err := dao.SeedTenantAdmin(tctx, role, admin); err != nil {
//	    _ = dao.HardDeleteTenant(ctx, t.ID) // 补偿：回滚租户行
//	    return nil, err
//	}
//
// 这段补偿本身有缺陷：补偿失败被 `_ =` 吞掉，会留下「有租户无超管」的
// 不可用状态；且补偿与原始写不在同一事务，并发下可能误删他人数据。
//
// 现在事务边界由 TxManager 声明，租户行、角色行、管理员行、角色绑定
// 与领域事件在同一事务内提交——要么全成，要么全败。
//
// 注意：初始超管授权（casbin p/g 行）仍走事件投影，不在本事务内，
// 因此不再出现 "租户已创建，但超管授权写入失败，请重试" 的模糊错误。
func (c *TenantCommand) Create(ctx context.Context, cmd CreateTenantCmd) error {
	exists, err := c.tenants.CodeExists(ctx, cmd.Code, 0)
	if err != nil {
		return err
	}
	if exists {
		return errParam("租户编码已存在")
	}

	hash, err := hashPassword(cmd.AdminPassword, c.HashPassword)
	if err != nil {
		return err
	}

	now := c.now()
	t := &iam.Tenant{
		Name:          cmd.Name,
		Code:          cmd.Code,
		ContactName:   cmd.ContactName,
		ContactMobile: cmd.ContactMobile,
		ExpireTime:    cmd.ExpireTime,
		Remark:        cmd.Remark,
		Status:        iam.TenantStatusEnabled,
	}

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		if err := c.tenants.Save(ctx, t); err != nil {
			return err
		}

		// 租户内表的写仍需注入新租户上下文，TenantPlugin 依此填充 tenant_id。
		tctx := infratenant.With(ctx, t.ID, t.Code)

		role := &iam.Role{
			TenantID:  t.ID,
			Name:      "超级管理员",
			Code:      c.SuperAdminRole(),
			Type:      iam.RoleTypeBuiltin,
			DataScope: iam.DataScopeAll,
			Status:    1,
		}
		if err := c.roles.Save(tctx, role); err != nil {
			return err
		}

		admin := &iam.User{
			TenantID: t.ID,
			Username: cmd.AdminUsername,
			Password: hash,
			Nickname: cmd.AdminUsername,
			Status:   iam.UserStatusEnabled,
		}
		// 分配超管角色：记录 UserRolesAssigned 事件，投影器据此建 casbin g 行。
		admin.AssignRoles([]int64{role.ID}, []string{role.Code}, now)
		if err := c.users.Save(tctx, admin); err != nil {
			return err
		}

		// 初始超管授权（域内全通 p 行 + g 行）：记录事件交投影器执行。
		// 改造前在此处同步调用 authz.SeedTenantAdmin，失败即返回
		// "租户已创建，但超管授权写入失败，请重试"。
		role.RecordEvent(iam.TenantAdminSeeded{
			BaseEvent:   shared.BaseEvent{Time: now},
			TenantID:    t.ID,
			AdminUserID: admin.ID,
		})
		return c.roles.Save(tctx, role)
	})
}

// UpdateTenantCmd 更新租户命令。
type UpdateTenantCmd struct {
	TenantID      int64
	Name          string
	ContactName   string
	ContactMobile string
	ExpireTime    *time.Time
	Remark        string
}

// Update 更新租户。
//
// 业务规则：传了过期时间则据其推导状态（未来→启用，过去→停用），
// 未传则清空过期时间（永不过期）。该规则原先在 service/tenant.go:130-140
// 以 updates map 的形式表达，现提为领域方法。
func (c *TenantCommand) Update(ctx context.Context, cmd UpdateTenantCmd) error {
	t, err := c.tenants.Get(ctx, cmd.TenantID)
	if err != nil {
		return err
	}

	now := c.now()
	t.Change(iam.TenantUpdate{
		Name:          cmd.Name,
		ContactName:   cmd.ContactName,
		ContactMobile: cmd.ContactMobile,
		ExpireTime:    cmd.ExpireTime,
		Remark:        cmd.Remark,
	}, now)
	t.SyncStatusWithExpiry(now)

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.tenants.Save(ctx, t)
	})
}

// ChangeTenantStatusCmd 变更租户状态命令。
type ChangeTenantStatusCmd struct {
	TenantID int64
	Status   int8
}

// ChangeStatus 启停租户。
//
// 停用时聚合记录 TenantDisabled 事件，由投影器撤销该租户全部会话（全员下线），
// 主流程不再同步等待会话撤销完成。
func (c *TenantCommand) ChangeStatus(ctx context.Context, cmd ChangeTenantStatusCmd) error {
	t, err := c.tenants.Get(ctx, cmd.TenantID)
	if err != nil {
		return err
	}
	t.Change(iam.TenantUpdate{
		Name:          t.Name,
		ContactName:   t.ContactName,
		ContactMobile: t.ContactMobile,
		ExpireTime:    t.ExpireTime,
		Remark:        t.Remark,
		Status:        cmd.Status,
	}, c.now())

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.tenants.Save(ctx, t)
	})
}

// DeleteTenantCmd 删除租户命令。
type DeleteTenantCmd struct {
	TenantID int64
}

// Delete 软删租户。
func (c *TenantCommand) Delete(ctx context.Context, cmd DeleteTenantCmd) error {
	t, err := c.tenants.Get(ctx, cmd.TenantID)
	if err != nil {
		return err
	}
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.tenants.Delete(ctx, t, t.Code)
	})
}
