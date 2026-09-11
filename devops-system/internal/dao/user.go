// Package dao 数据访问层。
//
// P4 阶段：dao 内显式 tenant_id 过滤（ctx 由 Tenant 中间件注入）；
// P5 TenantPlugin 装配后改为回调自动过滤，dao 签名不变。
package dao

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"devops-infra/errs"

	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
)

// tenantOf 取 ctx 中的租户（缺失即 dao 误用，fail-fast）。
func tenantOf(ctx context.Context) (int64, error) {
	tid, ok := tenantdb.From(ctx)
	if !ok {
		return 0, errs.New(errs.CodeInternal, "dao 缺少租户上下文")
	}
	return tid, nil
}

// GetUserByUsername 租户内按用户名取用户。
func GetUserByUsername(ctx context.Context, tenantID int64, username string) (*model.User, error) {
	var u model.User
	err := tenantdb.DB(ctx).Where("tenant_id = ? AND username = ?", tenantID, username).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeUnauthorized, "用户名或密码错误")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	return &u, nil
}

// GetUserByID 按 ID 取用户（租户内）。
func GetUserByID(ctx context.Context, tenantID, userID int64) (*model.User, error) {
	var u model.User
	err := tenantdb.DB(ctx).Where("tenant_id = ? AND id = ?", tenantID, userID).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "用户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	return &u, nil
}

// TouchLogin 更新最后登录信息（尽力而为，失败不影响登录）。
func TouchLogin(ctx context.Context, tenantID, userID int64, ip string) {
	tenantdb.DB(ctx).Model(&model.User{}).
		Where("tenant_id = ? AND id = ?", tenantID, userID).
		Updates(map[string]any{
			"login_ip":   ip,
			"login_date": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		})
}

// GetUserRoles 用户在租户内的角色列表。
func GetUserRoles(ctx context.Context, tenantID, userID int64) ([]model.Role, error) {
	var roles []model.Role
	err := tenantdb.DB(ctx).
		Table("t_system_role AS r").
		Joins("JOIN t_system_user_role ur ON ur.role_id = r.id AND ur.tenant_id = r.tenant_id").
		Where("ur.tenant_id = ? AND ur.user_id = ?", tenantID, userID).
		Find(&roles).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色失败")
	}
	return roles, nil
}

// GetUserMenus 用户在租户内可见菜单（按角色聚合去重）。
// menuTypes 过滤菜单类型（1目录 2菜单 3按钮），status=0 正常。
func GetUserMenus(ctx context.Context, tenantID, userID int64, menuTypes []int8) ([]model.Menu, error) {
	var menus []model.Menu
	q := tenantdb.DB(ctx).
		Table("t_system_menu AS m").
		Joins("JOIN t_system_role_menu rm ON rm.menu_id = m.id").
		Joins("JOIN t_system_user_role ur ON ur.role_id = rm.role_id AND ur.tenant_id = rm.tenant_id").
		Where("ur.tenant_id = ? AND ur.user_id = ? AND m.status = 0 AND m.deleted_at IS NULL",
			tenantID, userID).
		Distinct()
	if len(menuTypes) > 0 {
		q = q.Where("m.type IN ?", menuTypes)
	}
	err := q.Find(&menus).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	return menus, nil
}
