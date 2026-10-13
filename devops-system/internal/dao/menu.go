// 菜单管理 dao：菜单为全局表，TenantPlugin 放行。
package dao

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/model"
	"github.com/stanley760/devOps/devops-system/internal/tenantdb"
)

// ListMenus 全量菜单（树由 service 内存组装；status 可筛选）。
func ListMenus(ctx context.Context, status *int8) ([]model.Menu, error) {
	db := tenantdb.DB(ctx).Model(&model.Menu{})
	if status != nil {
		db = db.Where("status = ?", *status)
	}
	var menus []model.Menu
	if err := db.Order("sort ASC, id ASC").Find(&menus).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	return menus, nil
}

// GetMenuByID 按 ID 取菜单。
func GetMenuByID(ctx context.Context, menuID int64) (*model.Menu, error) {
	var m model.Menu
	err := tenantdb.DB(ctx).Where("id = ?", menuID).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "菜单不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	return &m, nil
}

// MenuHasChildren 是否存在未删除子菜单。
func MenuHasChildren(ctx context.Context, menuID int64) (bool, error) {
	var cnt int64
	err := tenantdb.DB(ctx).Model(&model.Menu{}).
		Where("parent_id = ?", menuID).Count(&cnt).Error
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询子菜单失败")
	}
	return cnt > 0, nil
}

// MenuReferencedByRoles 菜单是否被角色引用（role_menu）。
func MenuReferencedByRoles(ctx context.Context, menuID int64) (bool, error) {
	var cnt int64
	err := tenantdb.DB(ctx).Model(&model.RoleMenu{}).
		Where("menu_id = ?", menuID).Count(&cnt).Error
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询菜单引用失败")
	}
	return cnt > 0, nil
}

// CreateMenu 创建菜单（主键发号）。
func CreateMenu(ctx context.Context, m *model.Menu) error {
	if err := tenantdb.DB(ctx).Create(m).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "创建菜单失败")
	}
	return nil
}

// UpdateMenu 更新菜单字段。
func UpdateMenu(ctx context.Context, menuID int64, updates map[string]any) error {
	res := tenantdb.DB(ctx).Model(&model.Menu{}).
		Where("id = ?", menuID).Updates(updates)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新菜单失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "菜单不存在")
	}
	return nil
}

// DeleteMenu 软删菜单（调用方保证无子节点且未被引用）。
func DeleteMenu(ctx context.Context, menuID int64) error {
	res := tenantdb.DB(ctx).Delete(&model.Menu{}, menuID)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除菜单失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "菜单不存在")
	}
	return nil
}

// MenusByIDs 按 ID 集合取菜单（校验归属 + 权限标识收集）。
func MenusByIDs(ctx context.Context, menuIDs []int64) ([]model.Menu, error) {
	if len(menuIDs) == 0 {
		return nil, nil
	}
	var menus []model.Menu
	err := tenantdb.DB(ctx).Where("id IN ?", menuIDs).Find(&menus).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	if len(menus) != len(menuIDs) {
		return nil, errs.New(errs.CodeParam, "存在无效的菜单")
	}
	return menus, nil
}

// RolesByIDs 按角色 ID 集合取角色（permission 变更联动重建 p 行用）。
func RolesByIDs(ctx context.Context, roleIDs []int64) ([]model.Role, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var roles []model.Role
	err := tenantdb.DB(ctx).Where("id IN ?", roleIDs).Find(&roles).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色失败")
	}
	return roles, nil
}
