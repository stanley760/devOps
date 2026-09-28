// 角色管理服务：CRUD + casbin p 行按域重建。
//
// 内置角色（type=1，如 super_admin）跳过 p 行重建：其通配 p 行（*, {tid}, *）
// 是种子赋予的域内全通语义，不随菜单分配变化，避免把超管锁死。
package service

import (
	"context"

	"gorm.io/gorm"

	"devops-infra/errs"

	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/dao"
	"devops-system/internal/datascope"
	"devops-system/internal/middleware"
	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
	"devops-system/internal/types"
)

// validateDataScope 数据权限口径校验：自定义部门必须携带可解析的非空部门集合
// （JSON 数组字符串，与 post_ids 存储格式一致）。
func validateDataScope(dataScope int8, deptIDs string) error {
	if dataScope != datascope.ScopeCustom {
		return nil
	}
	if len(datascope.ParseDeptIDs(deptIDs)) == 0 {
		return errs.New(errs.CodeParam, "自定义部门口径必须提供部门 ID 集合（data_scope_dept_ids，JSON 数组）")
	}
	return nil
}

// Role 角色管理服务。
type Role struct{}

// NewRole 创建服务。
func NewRole() *Role { return &Role{} }

// List 分页查询。
func (s *Role) List(ctx context.Context, q types.RolePageQuery) ([]types.RoleDTO, int64, error) {
	roles, total, err := dao.ListRoles(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]types.RoleDTO, 0, len(roles))
	for i := range roles {
		out = append(out, toRoleDTO(&roles[i]))
	}
	return out, total, nil
}

// Get 角色详情（含已分配菜单 ID）。
func (s *Role) Get(ctx context.Context, id types.ID) (*types.RoleDetailResp, error) {
	r, err := dao.GetRoleByID(ctx, id.Int64())
	if err != nil {
		return nil, err
	}
	menuIDs, err := dao.GetRoleMenuIDs(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	mids := make([]types.ID, 0, len(menuIDs))
	for _, mid := range menuIDs {
		mids = append(mids, types.ID(mid))
	}
	return &types.RoleDetailResp{RoleDTO: toRoleDTO(r), MenuIDs: mids}, nil
}

// Create 创建角色：code 租户内唯一 → 发号落库（可选初始菜单）。
func (s *Role) Create(ctx context.Context, claims *auth.AccessClaims, req types.RoleSaveReq) error {
	if req.Code == "" {
		return errs.New(errs.CodeParam, "角色编码不能为空")
	}
	exists, err := dao.RoleCodeExists(ctx, req.Code, 0)
	if err != nil {
		return err
	}
	if exists {
		return errs.New(errs.CodeParam, "角色编码已存在")
	}
	if err := validateDataScope(req.DataScope, req.DataScopeDeptIDs); err != nil {
		return err
	}

	r := &model.Role{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		},
		Name:             req.Name,
		Code:             req.Code,
		Sort:             req.Sort,
		DataScope:        req.DataScope,
		DataScopeDeptIDs: datascope.NormalizeDeptIDs(req.DataScopeDeptIDs),
		Status:           req.Status,
		Type:             2, // 自定义
		Remark:           req.Remark,
	}
	if r.DataScope == 0 {
		r.DataScope = 1
	}
	
	return dao.CreateRole(ctx, r)
}

// Update 更新角色（code 不可改，避免 casbin p/g 行失联）。
func (s *Role) Update(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.RoleSaveReq) error {
	r, err := dao.GetRoleByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	dataScope := req.DataScope
	if dataScope == 0 {
		dataScope = r.DataScope
	}
	// 口径未变且未传部门集合时保留存量（data_scope_dept_ids 随字段整体覆盖，空入参会清空）。
	// 落库前 NormalizeDeptIDs 统一为数字数组形态，杜绝同一语义多种写法入库。
	deptIDs := req.DataScopeDeptIDs
	if deptIDs == "" && dataScope == r.DataScope {
		deptIDs = r.DataScopeDeptIDs
	}
	deptIDs = datascope.NormalizeDeptIDs(deptIDs)
	if err := validateDataScope(dataScope, deptIDs); err != nil {
		return err
	}
	updates := map[string]any{
		"name":                req.Name,
		"sort":                req.Sort,
		"data_scope":          dataScope,
		"data_scope_dept_ids": deptIDs,
		"status":              req.Status,
		"remark":              req.Remark,
		"updater":             claims.Username,
	}
	if err := tenantdb.DB(ctx).Model(&model.Role{}).
		Where("id = ?", r.ID).Updates(updates).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "更新角色失败")
	}
	// 数据权限口径已变：清缓存（该角色下所有用户下次请求重新解析）
	middleware.InvalidateDataScope()
	return nil
}

// AssignMenus 分配菜单：role_menu 重建 + p 行按 (role, tenant) 快照重建（即时生效）。
func (s *Role) AssignMenus(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.RoleAssignMenusReq) error {
	r, err := dao.GetRoleByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	menuIDs := idsToInt64(req.MenuIDs)
	menus, err := dao.MenusByIDs(ctx, menuIDs)
	if err != nil {
		return err
	}

	if err := tenantdb.DB(ctx).Transaction(func(tx *gorm.DB) error {
		return dao.SyncRoleMenusTx(tx, r.ID, menuIDs)
	}); err != nil {
		return err
	}

	if r.Type == 1 {
		return nil // 内置角色保持域内通配，不派生 p 行
	}
	perms := make([]string, 0, len(menus))
	for _, m := range menus {
		if m.Permission != "" {
			perms = append(perms, m.Permission)
		}
	}
	if err := authz.RebuildRolePolicies(r.Code, claims.TenantID, perms); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "菜单已保存，但权限策略刷新失败，请重试")
	}
	return nil
}

// Delete 删除角色：内置不可删 → 清关联（user_role/role_menu）→ 清 p 行与 g 行引用。
func (s *Role) Delete(ctx context.Context, claims *auth.AccessClaims, id types.ID) error {
	r, err := dao.GetRoleByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	if r.Type == 1 {
		return errs.New(errs.CodeParam, "内置角色不可删除")
	}

	if err := tenantdb.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dao.SoftDeleteRoleTx(tx, r.ID, r.Code); err != nil {
			return err
		}
		if err := dao.ClearRoleBindingsTx(tx, r.ID); err != nil {
			return errs.Wrap(err, errs.CodeInternal, "清理用户角色失败")
		}
		return tx.Where("role_id = ?", r.ID).Delete(&model.RoleMenu{}).Error
	}); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除角色失败")
	}

	if err := authz.RemoveRolePolicies(r.Code, claims.TenantID); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "角色已删除，但权限策略清理失败")
	}
	if err := authz.RemoveRoleGrouping(r.Code, claims.TenantID); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "角色已删除，但用户绑定清理失败")
	}
	// 角色删除影响其下用户的数据权限：清缓存重新解析
	middleware.InvalidateDataScope()
	return nil
}

// toRoleDTO model.Role → DTO。
func toRoleDTO(r *model.Role) types.RoleDTO {
	return types.RoleDTO{
		ID:               types.ID(r.ID),
		Name:             r.Name,
		Code:             r.Code,
		Sort:             r.Sort,
		DataScope:        r.DataScope,
		DataScopeDeptIDs: r.DataScopeDeptIDs,
		Status:           r.Status,
		Type:             r.Type,
		Remark:           r.Remark,
		CreateTime:       r.CreateTime,
	}
}
