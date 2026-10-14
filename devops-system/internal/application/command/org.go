package command

import (
	"context"

	"github.com/stanley760/devOps/devops-infra/datascope"

	"github.com/stanley760/devOps/devops-system/internal/domain/iam"
	"github.com/stanley760/devOps/devops-system/internal/domain/shared"
)

// RoleCommand 角色写侧命令处理器。
type RoleCommand struct {
	roles iam.RoleRepository
	menus iam.MenuRepository
	txs   shared.TxManager
	now   shared.NowFunc
}

// NewRoleCommand 构造角色命令处理器。
func NewRoleCommand(roles iam.RoleRepository, menus iam.MenuRepository, txs shared.TxManager) *RoleCommand {
	return &RoleCommand{roles: roles, menus: menus, txs: txs, now: shared.SystemNow}
}

// CreateRoleCmd 创建角色命令。
type CreateRoleCmd struct {
	TenantID         int64
	Name             string
	Code             string
	Sort             int
	DataScope        int8
	DataScopeDeptIDs string
	Status           int8
	Remark           string
}

// CreateRole 创建角色。
//
// 规则：code 非空 + 租户内唯一 + 数据权限口径合法（均在聚合内校验）。
func (c *RoleCommand) Create(ctx context.Context, cmd CreateRoleCmd) error {
	exists, err := c.roles.CodeExists(ctx, cmd.Code, 0)
	if err != nil {
		return err
	}
	if exists {
		return errParam("角色编码已存在")
	}

	scope := cmd.DataScope
	if scope == 0 {
		scope = iam.DataScopeDefaultDef
	}
	deptIDs := datascope.NormalizeDeptIDs(cmd.DataScopeDeptIDs)

	r := &iam.Role{
		TenantID:         cmd.TenantID,
		Name:             cmd.Name,
		Code:             cmd.Code,
		Sort:             cmd.Sort,
		DataScope:        scope,
		DataScopeDeptIDs: deptIDs,
		Status:           cmd.Status,
		Type:             iam.RoleTypeCustom,
		Remark:           cmd.Remark,
	}
	if err := r.ValidateForCreate(); err != nil {
		return err
	}

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.roles.Save(ctx, r)
	})
}

// UpdateRoleCmd 更新角色命令。
//
// 注意：不含 Code 字段——编码创建后不可变，避免 casbin p/g 行失联。
// 改造前 service/role.go:126 靠「updates map 里不写 code 键」隐式忽略，
// 隐式约定极易在后续迭代中被误改，故由类型层面根除。
type UpdateRoleCmd struct {
	TenantID         int64
	RoleID           int64
	Name             string
	Sort             int
	DataScope        int8
	DataScopeDeptIDs string
	Status           int8
	Remark           string
}

// UpdateRole 更新角色。
func (c *RoleCommand) Update(ctx context.Context, cmd UpdateRoleCmd) error {
	r, err := c.roles.Get(ctx, cmd.RoleID)
	if err != nil {
		return err
	}
	up := r.ResolveUpdate(iam.RoleUpdate{
		Name:             cmd.Name,
		Sort:             cmd.Sort,
		DataScope:        cmd.DataScope,
		DataScopeDeptIDs: cmd.DataScopeDeptIDs,
		Status:           cmd.Status,
		Remark:           cmd.Remark,
	}, datascope.NormalizeDeptIDs)
	if err := iam.ValidateDataScope(up.DataScope, up.DataScopeDeptIDs); err != nil {
		return err
	}
	r.Change(up, c.now())

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.roles.Save(ctx, r)
	})
}

// AssignRoleMenusCmd 分配菜单命令。
type AssignRoleMenusCmd struct {
	TenantID int64
	RoleID   int64
	MenuIDs  []int64
}

// AssignMenus 分配菜单（重建 role_menu + 派生 casbin p 行）。
//
// 内置角色不派生 p 行——该判断在聚合内（Role.AssignMenus），
// 保证「超管不被菜单分配锁死」这条规则无法在应用层被绕过。
func (c *RoleCommand) AssignMenus(ctx context.Context, cmd AssignRoleMenusCmd) error {
	r, err := c.roles.Get(ctx, cmd.RoleID)
	if err != nil {
		return err
	}

	// 取权限标识而非加载完整菜单聚合：写路径上的高频操作。
	perms, err := c.roles.PermissionsByMenuIDs(ctx, cmd.MenuIDs)
	if err != nil {
		return err
	}

	now := c.now()
	r.AssignMenus(cmd.MenuIDs, perms, now)

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.roles.Save(ctx, r)
	})
}

// DeleteRoleCmd 删除角色命令。
type DeleteRoleCmd struct {
	TenantID int64
	RoleID   int64
}

// DeleteRole 软删角色（内置角色拒绝）。
//
// 角色行 + user_role + role_menu 在同一事务内清理，
// casbin p 行与 g 行清理由 RoleDeleted 事件的投影器完成。
func (c *RoleCommand) Delete(ctx context.Context, cmd DeleteRoleCmd) error {
	r, err := c.roles.Get(ctx, cmd.RoleID)
	if err != nil {
		return err
	}
	if err := r.EnsureDeletable(); err != nil {
		return err
	}
	r.MarkDeleted(c.now())

	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.roles.Delete(ctx, r, r.Code)
	})
}

// OrgCommand 部门与岗位写侧命令处理器。
type OrgCommand struct {
	depts iam.DeptRepository
	posts iam.PostRepository
	menus iam.MenuRepository
	txs   shared.TxManager
	now   shared.NowFunc
}

// NewOrgCommand 构造组织架构命令处理器。
func NewOrgCommand(depts iam.DeptRepository, posts iam.PostRepository, menus iam.MenuRepository, txs shared.TxManager) *OrgCommand {
	return &OrgCommand{depts: depts, posts: posts, menus: menus, txs: txs, now: shared.SystemNow}
}

// SaveDeptCmd 保存部门命令（创建与更新合一：code 不可变，故可合流）。
type SaveDeptCmd struct {
	TenantID     int64
	DeptID       int64 // 0 表示创建
	ParentID     int64
	Name         string
	Sort         int
	LeaderUserID int64
	Phone        string
	Email        string
	Status       int8
}

// SaveDept 创建或更新部门。
func (c *OrgCommand) SaveDept(ctx context.Context, cmd SaveDeptCmd) error {
	if cmd.DeptID == 0 {
		d := &iam.Dept{
			TenantID:     cmd.TenantID,
			ParentID:     cmd.ParentID,
			Name:         cmd.Name,
			Sort:         cmd.Sort,
			LeaderUserID: cmd.LeaderUserID,
			Phone:        cmd.Phone,
			Email:        cmd.Email,
			Status:       cmd.Status,
		}
		if err := d.ValidateForCreate(); err != nil {
			return err
		}
		return c.txs.WithinTx(ctx, func(ctx context.Context) error {
			return c.depts.Save(ctx, d)
		})
	}

	d, err := c.depts.Get(ctx, cmd.DeptID)
	if err != nil {
		return err
	}
	if err := d.EnsureNotSelf(cmd.ParentID); err != nil {
		return err
	}
	d.Change(iam.DeptUpdate{
		ParentID:     cmd.ParentID,
		Name:         cmd.Name,
		Sort:         cmd.Sort,
		LeaderUserID: cmd.LeaderUserID,
		Phone:        cmd.Phone,
		Email:        cmd.Email,
		Status:       cmd.Status,
	})
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.depts.Save(ctx, d)
	})
}

// DeleteDeptCmd 删除部门命令。
type DeleteDeptCmd struct {
	TenantID int64
	DeptID   int64
}

// DeleteDept 软删部门（有下级或成员时拒绝）。
func (c *OrgCommand) DeleteDept(ctx context.Context, cmd DeleteDeptCmd) error {
	d, err := c.depts.Get(ctx, cmd.DeptID)
	if err != nil {
		return err
	}
	hasChildren, err := c.depts.HasChildren(ctx, d.ID)
	if err != nil {
		return err
	}
	if hasChildren {
		return errParam("存在下级部门，不能删除")
	}
	hasMembers, err := c.depts.HasMembers(ctx, d.ID)
	if err != nil {
		return err
	}
	if hasMembers {
		return errParam("部门下存在用户，不能删除")
	}
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.depts.Delete(ctx, d, d.Name)
	})
}

// SavePostCmd 保存岗位命令。
type SavePostCmd struct {
	TenantID int64
	PostID   int64 // 0 表示创建
	Code     string
	Name     string
	Sort     int
	Status   int8
	Remark   string
}

// SavePost 创建或更新岗位。
func (c *OrgCommand) SavePost(ctx context.Context, cmd SavePostCmd) error {
	if cmd.PostID == 0 {
		exists, err := c.posts.CodeExists(ctx, cmd.Code, 0)
		if err != nil {
			return err
		}
		if exists {
			return errParam("岗位编码已存在")
		}
		p := &iam.Post{
			TenantID: cmd.TenantID,
			Code:     cmd.Code,
			Name:     cmd.Name,
			Sort:     cmd.Sort,
			Status:   cmd.Status,
			Remark:   cmd.Remark,
		}
		if err := p.ValidateForCreate(); err != nil {
			return err
		}
		return c.txs.WithinTx(ctx, func(ctx context.Context) error {
			return c.posts.Save(ctx, p)
		})
	}

	p, err := c.posts.Get(ctx, cmd.PostID)
	if err != nil {
		return err
	}
	p.Change(iam.PostUpdate{
		Code:   cmd.Code,
		Name:   cmd.Name,
		Sort:   cmd.Sort,
		Status: cmd.Status,
		Remark: cmd.Remark,
	})
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.posts.Save(ctx, p)
	})
}

// DeletePostCmd 删除岗位命令。
type DeletePostCmd struct {
	TenantID int64
	PostID   int64
}

// DeletePost 软删岗位。
func (c *OrgCommand) DeletePost(ctx context.Context, cmd DeletePostCmd) error {
	p, err := c.posts.Get(ctx, cmd.PostID)
	if err != nil {
		return err
	}
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.posts.Delete(ctx, p, p.Code)
	})
}

// SaveMenuCmd 保存菜单命令。
type SaveMenuCmd struct {
	MenuID        int64 // 0 表示创建
	Name          string
	Permission    string
	Type          int8
	Sort          int
	ParentID      int64
	Path          string
	Icon          string
	Component     string
	ComponentName string
	Status        int8
	Visible       bool
	KeepAlive     bool
	AlwaysShow    bool
	Remark        string
}

// SaveMenu 创建或更新菜单。
func (c *OrgCommand) SaveMenu(ctx context.Context, cmd SaveMenuCmd) error {
	if cmd.MenuID == 0 {
		m := &iam.Menu{
			Name:          cmd.Name,
			Permission:    cmd.Permission,
			Type:          cmd.Type,
			Sort:          cmd.Sort,
			ParentID:      cmd.ParentID,
			Path:          cmd.Path,
			Icon:          cmd.Icon,
			Component:     cmd.Component,
			ComponentName: cmd.ComponentName,
			Status:        cmd.Status,
			Visible:       cmd.Visible,
			KeepAlive:     cmd.KeepAlive,
			AlwaysShow:    cmd.AlwaysShow,
		}
		if err := m.ValidateForCreate(); err != nil {
			return err
		}
		return c.txs.WithinTx(ctx, func(ctx context.Context) error {
			return c.menus.Save(ctx, m)
		})
	}

	m, err := c.menus.Get(ctx, cmd.MenuID)
	if err != nil {
		return err
	}
	// 防环：不可挂到自己的子树下（改造前缺此校验，有死循环风险）。
	all, err := c.menus.ListAll(ctx, iam.MenuFilter{})
	if err != nil {
		return err
	}
	if err := m.EnsureNotSelfOrDescendant(cmd.ParentID, all); err != nil {
		return err
	}
	m.Change(iam.MenuUpdate{
		Name:          cmd.Name,
		Permission:    cmd.Permission,
		Type:          cmd.Type,
		Sort:          cmd.Sort,
		ParentID:      cmd.ParentID,
		Path:          cmd.Path,
		Icon:          cmd.Icon,
		Component:     cmd.Component,
		ComponentName: cmd.ComponentName,
		Status:        cmd.Status,
		Visible:       cmd.Visible,
		KeepAlive:     cmd.KeepAlive,
		AlwaysShow:    cmd.AlwaysShow,
		Remark:        cmd.Remark,
	})
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.menus.Save(ctx, m)
	})
}

// DeleteMenuCmd 删除菜单命令。
type DeleteMenuCmd struct {
	MenuID int64
}

// DeleteMenu 删除菜单。
func (c *OrgCommand) DeleteMenu(ctx context.Context, cmd DeleteMenuCmd) error {
	return c.txs.WithinTx(ctx, func(ctx context.Context) error {
		return c.menus.Delete(ctx, cmd.MenuID)
	})
}
