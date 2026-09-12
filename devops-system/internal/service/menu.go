// 菜单管理服务（P6）：CRUD + permission 变更联动重建引用角色的 p 行。
package service

import (
	"context"

	"devops-infra/errs"

	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/dao"
	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
	"devops-system/internal/types"
)

// Menu 菜单管理服务。
type Menu struct{}

// NewMenu 创建服务。
func NewMenu() *Menu { return &Menu{} }

// List 菜单树（status 筛选可选）。
func (s *Menu) List(ctx context.Context, status *int8) ([]types.MenuTreeResp, error) {
	menus, err := dao.ListMenus(ctx, status)
	if err != nil {
		return nil, err
	}
	return buildMenuTree(menus), nil
}

// Get 菜单详情。
func (s *Menu) Get(ctx context.Context, id types.ID) (*types.MenuDTO, error) {
	m, err := dao.GetMenuByID(ctx, id.Int64())
	if err != nil {
		return nil, err
	}
	dto := toMenuDTO(m)
	return &dto, nil
}

// Create 创建菜单：校验类型/父节点（目录挂 permission 无意义，按钮必须挂）。
func (s *Menu) Create(ctx context.Context, claims *auth.AccessClaims, req types.MenuSaveReq) error {
	if err := validateMenu(req.Type, req.ParentID.Int64(), req.Permission); err != nil {
		return err
	}
	if req.ParentID.Int64() > 0 {
		if _, err := dao.GetMenuByID(ctx, req.ParentID.Int64()); err != nil {
			return err
		}
	}

	m := &model.Menu{
		BaseModel: model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		Name:      req.Name, Permission: req.Permission, Type: req.Type, Sort: req.Sort,
		ParentID: req.ParentID.Int64(), Path: req.Path, Icon: req.Icon,
		Component: req.Component, ComponentName: req.ComponentName, Status: req.Status,
		Visible: derefBool(req.Visible, true), KeepAlive: derefBool(req.KeepAlive, false),
		AlwaysShow: derefBool(req.AlwaysShow, true),
	}
	if err := dao.CreateMenu(ctx, m); err != nil {
		return err
	}
	return nil
}

// Update 更新菜单：permission 变更时联动重建引用角色的 p 行（仅自定义角色）。
func (s *Menu) Update(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.MenuSaveReq) error {
	old, err := dao.GetMenuByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	if err := validateMenu(req.Type, req.ParentID.Int64(), req.Permission); err != nil {
		return err
	}
	if req.ParentID.Int64() == old.ID {
		return errs.New(errs.CodeParam, "父菜单不能是自身")
	}
	if req.ParentID.Int64() > 0 {
		if _, err := dao.GetMenuByID(ctx, req.ParentID.Int64()); err != nil {
			return err
		}
	}

	updates := map[string]any{
		"name": req.Name, "permission": req.Permission, "type": req.Type, "sort": req.Sort,
		"parent_id": req.ParentID.Int64(), "path": req.Path, "icon": req.Icon,
		"component": req.Component, "component_name": req.ComponentName, "status": req.Status,
		"updater": claims.Username,
	}
	// bool 指针缺省时保留原值（map 更新无法区分零值，显式处理）
	if req.Visible != nil {
		updates["visible"] = *req.Visible
	}
	if req.KeepAlive != nil {
		updates["keep_alive"] = *req.KeepAlive
	}
	if req.AlwaysShow != nil {
		updates["always_show"] = *req.AlwaysShow
	}
	if err := dao.UpdateMenu(ctx, old.ID, updates); err != nil {
		return err
	}

	if old.Permission != req.Permission {
		return s.rebuildReferencingRoles(ctx, old.ID)
	}
	return nil
}

// Delete 删除菜单：有子菜单/被角色引用不可删。
func (s *Menu) Delete(ctx context.Context, id types.ID) error {
	menuID := id.Int64()
	if _, err := dao.GetMenuByID(ctx, menuID); err != nil {
		return err
	}
	has, err := dao.MenuHasChildren(ctx, menuID)
	if err != nil {
		return err
	}
	if has {
		return errs.New(errs.CodeParam, "存在子菜单，无法删除")
	}
	has, err = dao.MenuReferencedByRoles(ctx, menuID)
	if err != nil {
		return err
	}
	if has {
		return errs.New(errs.CodeParam, "菜单已被角色引用，请先取消分配")
	}
	return dao.DeleteMenu(ctx, menuID)
}

// rebuildReferencingRoles permission 变更后：按本租户引用该菜单的角色重建 p 行。
func (s *Menu) rebuildReferencingRoles(ctx context.Context, menuID int64) error {
	roleIDs, err := dao.RoleIDsByMenuIDs(ctx, []int64{menuID})
	if err != nil {
		return err
	}
	roles, err := dao.RolesByIDs(ctx, roleIDs)
	if err != nil {
		return err
	}
	tid, ok := tenantOfCtx(ctx)
	if !ok {
		return errs.New(errs.CodeInternal, "缺少租户上下文")
	}
	for i := range roles {
		r := &roles[i]
		if r.Type == 1 {
			continue // 内置角色保持通配
		}
		menuIDs, err := dao.GetRoleMenuIDs(ctx, r.ID)
		if err != nil {
			return err
		}
		menus, err := dao.MenusByIDs(ctx, menuIDs)
		if err != nil {
			return err
		}
		perms := make([]string, 0, len(menus))
		for _, m := range menus {
			if m.Permission != "" {
				perms = append(perms, m.Permission)
			}
		}
		if err := authz.RebuildRolePolicies(r.Code, tid, perms); err != nil {
			return errs.Wrap(err, errs.CodeInternal, "重建角色权限策略失败")
		}
	}
	return nil
}

// tenantOfCtx 从 ctx 取租户（service 层便捷封装）。
func tenantOfCtx(ctx context.Context) (int64, bool) {
	tc, ok := tenantdb.From(ctx)
	return tc.ID, ok
}

// validateMenu 菜单约束：类型合法；按钮必须挂权限标识；目录不挂（留空）。
func validateMenu(typ int8, parentID int64, permission string) error {
	switch typ {
	case 1: // 目录
		if permission != "" {
			return errs.New(errs.CodeParam, "目录不挂权限标识")
		}
	case 2: // 菜单
		if parentID == 0 {
			return errs.New(errs.CodeParam, "菜单必须挂在目录下")
		}
	case 3: // 按钮
		if permission == "" {
			return errs.New(errs.CodeParam, "按钮必须配置权限标识")
		}
		if parentID == 0 {
			return errs.New(errs.CodeParam, "按钮必须挂在菜单下")
		}
	default:
		return errs.New(errs.CodeParam, "菜单类型非法（1目录 2菜单 3按钮）")
	}
	return nil
}

// buildMenuTree 列表 → 树（parent 不在集合或为 0 即根；按 sort 排序已由查询保证）。
func buildMenuTree(menus []model.Menu) []types.MenuTreeResp {
	byID := make(map[int64]*types.MenuTreeResp, len(menus))
	roots := make([]*types.MenuTreeResp, 0)
	for i := range menus {
		node := &types.MenuTreeResp{MenuDTO: toMenuDTO(&menus[i])}
		byID[menus[i].ID] = node
	}
	for i := range menus {
		m := &menus[i]
		node := byID[m.ID]
		if parent, ok := byID[m.ParentID]; ok && m.ParentID != m.ID {
			parent.Children = append(parent.Children, *node)
		} else {
			roots = append(roots, node)
		}
	}
	out := make([]types.MenuTreeResp, 0, len(roots))
	for _, n := range roots {
		out = append(out, *n)
	}
	return out
}

// toMenuDTO model.Menu → DTO。
func toMenuDTO(m *model.Menu) types.MenuDTO {
	return types.MenuDTO{
		ID: types.ID(m.ID), Name: m.Name, Permission: m.Permission, Type: m.Type,
		Sort: m.Sort, ParentID: types.ID(m.ParentID), Path: m.Path, Icon: m.Icon,
		Component: m.Component, ComponentName: m.ComponentName, Status: m.Status,
		Visible: m.Visible, KeepAlive: m.KeepAlive, AlwaysShow: m.AlwaysShow,
		CreateTime: m.CreateTime,
	}
}

// derefBool 指针缺省取 def。
func derefBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
