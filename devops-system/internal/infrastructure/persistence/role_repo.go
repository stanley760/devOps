package persistence

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-common/domain"
	"github.com/stanley760/devOps/devops-common/domain/gormx"
	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/domain/iam"
	"github.com/stanley760/devOps/devops-system/internal/model"
)

// roleRepo 是 iam.RoleRepository 的 GORM 实现。
type roleRepo struct{}

// NewRoleRepository 构造角色仓储。
func NewRoleRepository() iam.RoleRepository { return roleRepo{} }

func (roleRepo) Save(ctx context.Context, r *iam.Role) error {
	if r.ID == 0 {
		if err := db(ctx).Create(toRolePO(r)).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建角色失败")
		}
	} else {
		res := db(ctx).Model(&model.Role{}).Where("id = ?", r.ID).Updates(map[string]any{
			"name":                r.Name,
			"sort":                r.Sort,
			"data_scope":          r.DataScope,
			"data_scope_dept_ids": r.DataScopeDeptIDs,
			"status":              r.Status,
			"remark":              r.Remark,
		})
		if res.Error != nil {
			return errs.Wrap(res.Error, errs.CodeInternal, "更新角色失败")
		}
		if res.RowsAffected == 0 {
			return errs.New(errs.CodeNotFound, "角色不存在")
		}
	}
	if r.MenuIDs != nil {
		if err := replaceRoleMenus(ctx, r.ID, r.MenuIDs); err != nil {
			return err
		}
	}
	return appendEvents(ctx, r.PullEvents()...)
}

func (roleRepo) Get(ctx context.Context, id int64) (*iam.Role, error) {
	var po model.Role
	if err := db(ctx).Where("id = ?", id).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "角色不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色失败")
	}
	r := toRoleDomain(&po)
	menuIDs, err := roleMenuIDs(ctx, po.ID)
	if err != nil {
		return nil, err
	}
	r.MenuIDs = menuIDs
	return r, nil
}

func (roleRepo) List(ctx context.Context, f iam.RoleFilter) (domain.PageResult[*iam.Role], error) {
	conn := db(ctx).Model(&model.Role{})
	if f.Name != "" {
		conn = conn.Where("name LIKE ?", "%"+f.Name+"%")
	}
	if f.Code != "" {
		conn = conn.Where("code = ?", f.Code)
	}
	if f.Status != nil {
		conn = conn.Where("status = ?", *f.Status)
	}
	page, err := gormx.FindPage[model.Role](conn.Order("sort ASC, id ASC"), f.PageParam)
	if err != nil {
		return domain.PageResult[*iam.Role]{}, errs.Wrap(err, errs.CodeInternal, "查询角色列表失败")
	}
	return domain.MapPage(page, func(po model.Role) *iam.Role { return toRoleDomain(&po) }), nil
}

// Delete 软删角色并清理用户/菜单关联。
//
// code 改写为 xxx_del_{id} 以绕开租户内唯一索引 uk_tenant_code，
// 使同编码角色可重新创建（存储契约，重构后口径不变）。
func (roleRepo) Delete(ctx context.Context, r *iam.Role, operator string) error {
	conn := db(ctx)
	now := time.Now()

	res := conn.Model(&model.Role{}).Where("id = ?", r.ID).Updates(map[string]any{
		"code":       iam.SoftDeletedCode(r.Code, r.ID),
		"deleted_at": now,
		"updater":    operator,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除角色失败")
	}
	if err := conn.Where("role_id = ?", r.ID).Delete(&model.UserRole{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "清理用户角色失败")
	}
	if err := conn.Where("role_id = ?", r.ID).Delete(&model.RoleMenu{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除角色失败")
	}
	return appendEvents(ctx, r.PullEvents()...)
}

func (roleRepo) CodeExists(ctx context.Context, code string, excludeID int64) (bool, error) {
	return existsBy(ctx, &model.Role{}, "code", code, excludeID, "检查角色编码唯一性失败")
}

func (roleRepo) ReplaceMenus(ctx context.Context, roleID int64, menuIDs []int64) error {
	return replaceRoleMenus(ctx, roleID, menuIDs)
}

func replaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64) error {
	conn := db(ctx)
	if err := conn.Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "清理角色菜单失败")
	}
	if len(menuIDs) == 0 {
		return nil
	}
	rows := make([]model.RoleMenu, 0, len(menuIDs))
	for _, mid := range menuIDs {
		rows = append(rows, model.RoleMenu{RoleID: roleID, MenuID: mid})
	}
	if err := conn.Create(&rows).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "保存角色菜单失败")
	}
	return nil
}

func (roleRepo) MenusByIDs(ctx context.Context, menuIDs []int64) ([]*iam.Menu, error) {
	if len(menuIDs) == 0 {
		return nil, nil
	}
	var pos []model.Menu
	if err := db(ctx).Where("id IN ?", menuIDs).Find(&pos).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	out := make([]*iam.Menu, 0, len(pos))
	for i := range pos {
		out = append(out, toMenuDomain(&pos[i]))
	}
	return out, nil
}

// PermissionsByMenuIDs 批量取权限标识。
//
// 只取 permission 列而非加载完整菜单聚合：分配菜单是写路径上的高频操作，
// 此处避免为取一个字符串字段而构造完整聚合。
func (roleRepo) PermissionsByMenuIDs(ctx context.Context, menuIDs []int64) ([]string, error) {
	if len(menuIDs) == 0 {
		return nil, nil
	}
	var perms []string
	if err := db(ctx).Model(&model.Menu{}).
		Where("id IN ?", menuIDs).
		Where("permission != ''").Distinct().Pluck("permission", &perms).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单权限失败")
	}
	return perms, nil
}

func roleMenuIDs(ctx context.Context, roleID int64) ([]int64, error) {
	var ids []int64
	if err := db(ctx).Model(&model.RoleMenu{}).
		Where("role_id = ?", roleID).Pluck("menu_id", &ids).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色菜单失败")
	}
	return ids, nil
}

func toRolePO(r *iam.Role) *model.Role {
	return &model.Role{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: r.Code, Updater: r.Code},
			TenantID:  r.TenantID,
		},
		Name:             r.Name,
		Code:             r.Code,
		Sort:             r.Sort,
		DataScope:        r.DataScope,
		DataScopeDeptIDs: r.DataScopeDeptIDs,
		Status:           r.Status,
		Type:             r.Type,
		Remark:           r.Remark,
	}
}

func toRoleDomain(po *model.Role) *iam.Role {
	return &iam.Role{
		ID:               po.ID,
		TenantID:         po.TenantID,
		Name:             po.Name,
		Code:             po.Code,
		Sort:             po.Sort,
		DataScope:        po.DataScope,
		DataScopeDeptIDs: po.DataScopeDeptIDs,
		Status:           po.Status,
		Type:             po.Type,
		Remark:           po.Remark,
		CreateTime:       po.CreateTime,
		UpdateTime:       po.UpdateTime,
	}
}
