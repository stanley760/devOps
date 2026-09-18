// Package dao 数据访问层。
//
// 租户过滤双保险：纯模型查询由 TenantPlugin 自动注入（tenantdb.DB(ctx)）；
// 原生 JOIN 查询（多表下插件注入裸列会歧义）由 dao 显式带 tenant_id。
// 租户 ID 统一取自 ctx（middleware.Tenant 注入；Login/Refresh 解析租户后注入）。
package dao

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"devops-infra/errs"

	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
	infratenant "devops-infra/tenant"
)

// tenantOf 取 ctx 中的租户（原生 JOIN 查询显式过滤用；缺失即调用方误用，fail-fast）。
func tenantOf(ctx context.Context) (int64, error) {
	tc, ok := infratenant.From(ctx)
	if !ok {
		return 0, errs.New(errs.CodeInternal, "dao 缺少租户上下文")
	}
	return tc.ID, nil
}

// GetUserByUsername 按用户名取用户（tenant_id 由插件注入）。
func GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := tenantdb.DB(ctx).Where("username = ?", username).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeUnauthorized, "用户名或密码错误")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	return &u, nil
}

// GetUserByID 按 ID 取用户（tenant_id 由插件注入）。
func GetUserByID(ctx context.Context, userID int64) (*model.User, error) {
	var u model.User
	err := tenantdb.DB(ctx).Where("id = ?", userID).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "用户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	return &u, nil
}

// TouchLogin 更新最后登录信息（尽力而为，失败不影响登录）。
func TouchLogin(ctx context.Context, userID int64, ip string) {
	tenantdb.DB(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"login_ip":   ip,
			"login_date": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		})
}

// GetUserRoles 用户在租户内的角色列表（原生 JOIN，显式过滤）。
func GetUserRoles(ctx context.Context, userID int64) ([]model.Role, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var roles []model.Role
	err = tenantdb.DB(ctx).
		Table("t_system_role AS r").
		Joins("JOIN t_system_user_role ur ON ur.role_id = r.id AND ur.tenant_id = r.tenant_id").
		Where("ur.tenant_id = ? AND ur.user_id = ?", tid, userID).
		Find(&roles).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色失败")
	}
	return roles, nil
}

// GetUserMenus 用户在租户内可见菜单（按角色聚合去重，原生 JOIN 显式过滤）。
// menuTypes 过滤菜单类型（1目录 2菜单 3按钮），status=0 正常。
func GetUserMenus(ctx context.Context, userID int64, menuTypes []int8) ([]model.Menu, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var menus []model.Menu
	q := tenantdb.DB(ctx).
		Table("t_system_menu AS m").
		Joins("JOIN t_system_role_menu rm ON rm.menu_id = m.id").
		Joins("JOIN t_system_user_role ur ON ur.role_id = rm.role_id AND ur.tenant_id = rm.tenant_id").
		Where("ur.tenant_id = ? AND ur.user_id = ? AND m.status = 0 AND m.deleted_at IS NULL",
			tid, userID).
		Distinct()
	if len(menuTypes) > 0 {
		q = q.Where("m.type IN ?", menuTypes)
	}
	err = q.Find(&menus).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	return menus, nil
}
