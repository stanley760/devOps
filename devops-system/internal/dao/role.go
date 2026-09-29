// 角色管理 dao（P6）：p 行重建相关见 service 层（casbin 与 DB 事务边界）。
package dao

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"

	"devops-infra/errs"

	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
	"devops-system/internal/types"
)

// ListRoles 分页查询角色（筛选 name/code/status）。
func ListRoles(ctx context.Context, q types.RolePageQuery) ([]model.Role, int64, error) {
	db := tenantdb.DB(ctx).Model(&model.Role{})
	if q.Name != "" {
		db = db.Where("name LIKE ?", "%"+q.Name+"%")
	}
	if q.Code != "" {
		db = db.Where("code = ?", q.Code)
	}
	if q.Status != nil {
		db = db.Where("status = ?", *q.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "查询角色总数失败")
	}
	pageNum, pageSize := pageOf(q.PageNum, q.PageSize)
	var roles []model.Role
	if err := db.Order("sort ASC, id ASC").
		Offset((pageNum - 1) * pageSize).Limit(pageSize).
		Find(&roles).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "查询角色列表失败")
	}
	return roles, total, nil
}

// pageOf 分页参数默认值。
func pageOf(pageNum, pageSize int) (int, int) {
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	return pageNum, pageSize
}

// UpdateRole 更新角色字段（map 形式，仅非零字段）。
func UpdateRole(ctx context.Context, roleID int64, updates map[string]any) error {
	if err := tenantdb.DB(ctx).Model(&model.Role{}).
		Where("id = ?", roleID).Updates(updates).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "更新角色失败")
	}
	return nil
}

// GetRoleByID 按 ID 取角色（租户内）。
func GetRoleByID(ctx context.Context, roleID int64) (*model.Role, error) {
	var r model.Role
	err := tenantdb.DB(ctx).Where("id = ?", roleID).First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "角色不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色失败")
	}
	return &r, nil
}

// RoleCodeExists 租户内角色编码是否已占用（excludeID 排除自身）。
func RoleCodeExists(ctx context.Context, code string, excludeID int64) (bool, error) {
	db := tenantdb.DB(ctx).Model(&model.Role{}).Where("code = ?", code)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	var cnt int64
	if err := db.Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "检查角色编码唯一性失败")
	}
	return cnt > 0, nil
}

// CreateRole 创建角色（主键发号）。
func CreateRole(ctx context.Context, r *model.Role) error {
	if err := tenantdb.DB(ctx).Create(r).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "创建角色失败")
	}
	return nil
}

// SoftDeleteRoleTx 事务内软删角色：改写 code 绕开租户内唯一索引（同名可重建）。
func SoftDeleteRoleTx(tx *gorm.DB, roleID int64, code string) error {
	res := tx.Model(&model.Role{}).Where("id = ?", roleID).Updates(map[string]any{
		"code":       code + "_del_" + strconv.FormatInt(roleID, 10),
		"deleted_at": time.Now(),
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除角色失败")
	}
	return nil
}

// GetRoleMenuIDs 角色已分配的菜单 ID 列表（tenant_id 由插件注入）。
func GetRoleMenuIDs(ctx context.Context, roleID int64) ([]int64, error) {
	var ids []int64
	err := tenantdb.DB(ctx).Model(&model.RoleMenu{}).
		Where("role_id = ?", roleID).Pluck("menu_id", &ids).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色菜单失败")
	}
	return ids, nil
}

// SyncRoleMenusTx 事务内重建角色-菜单关联（物理删旧 + 批量插新）。
func SyncRoleMenusTx(tx *gorm.DB, roleID int64, menuIDs []int64) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "清理角色菜单失败")
	}
	if len(menuIDs) == 0 {
		return nil
	}
	rows := make([]model.RoleMenu, 0, len(menuIDs))
	for _, mid := range menuIDs {
		rows = append(rows, model.RoleMenu{RoleID: roleID, MenuID: mid})
	}
	return tx.Create(&rows).Error
}

// RoleIDsByMenuIDs 引用指定菜单集合的角色 ID 列表（重建 p 行用）。
func RoleIDsByMenuIDs(ctx context.Context, menuIDs []int64) ([]int64, error) {
	if len(menuIDs) == 0 {
		return nil, nil
	}
	var ids []int64
	err := tenantdb.DB(ctx).Model(&model.RoleMenu{}).
		Where("menu_id IN ?", menuIDs).Distinct().Pluck("role_id", &ids).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单引用角色失败")
	}
	return ids, nil
}

// ClearRoleBindingsTx 事务内清理角色的用户关联（删除角色时）。
func ClearRoleBindingsTx(tx *gorm.DB, roleID int64) error {
	return tx.Where("role_id = ?", roleID).Delete(&model.UserRole{}).Error
}

// ReplaceRoleMenus 重建角色的菜单关联（单事务）。
func ReplaceRoleMenus(ctx context.Context, roleID int64, menuIDs []int64) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		return SyncRoleMenusTx(tx, roleID, menuIDs)
	})
}

// DeleteRole 删除角色（单事务）：软删（改写 code）→ 清用户关联 → 清菜单关联。
func DeleteRole(ctx context.Context, r *model.Role) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		if err := SoftDeleteRoleTx(tx, r.ID, r.Code); err != nil {
			return err
		}
		if err := ClearRoleBindingsTx(tx, r.ID); err != nil {
			return errs.Wrap(err, errs.CodeInternal, "清理用户角色失败")
		}
		if err := tx.Where("role_id = ?", r.ID).Delete(&model.RoleMenu{}).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "删除角色失败")
		}
		return nil
	})
}
