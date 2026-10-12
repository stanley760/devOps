// Package authz 管理面权限门面：Enforce 与策略维护统一路由到共享 authorizer
// （devops-infra/security/authz，实例化 Casbin + gorm-adapter，无包级 enforcer）。
//
// 模型要点（由共享包承载，行为不变）：
//   - sub = user_id、dom = tenant_id、obj = 权限标识（如 system:user:create）
//   - g(user, role, tenant) 三元组：租户内角色，user_id 在不同租户是不同 g 行，天然隔离
//   - g2(user, platform_admin) 二元组：平台角色（不绑域）——域内 * 与平台能力彻底分离
//   - p(role, tenant, perm)：角色在某租户的权限快照；p, platform_admin, *, * 平台管理员跨域全通
//
// 防越权边界（6.3）：platform:tenant:* 权限标识只出现在平台路由；
// 租户超管的 p, super_admin, {tid}, * 因 dom 不匹配 "*" 无法触达平台接口。
//
// 本包仅保留管理面语义：角色名配置（RoleConfig）、种子 ID 与
// 数字主键/域转换在系统侧完成；通用持久化与鉴权决策在共享包。
package authz

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"

	"gorm.io/gorm"

	securityauthz "devops-infra/security/authz"
)

// holder 显式初始化的实例持有者（装配链：Init/SetAuthorizer → 各包级函数）。
// 测试可用 SetAuthorizer 替换，实例间不串扰。
var holder atomic.Pointer[securityauthz.CasbinAuthorizer]

// Init 装配共享 authorizer：gorm-adapter 持久化（casbin_rule 表）+ 加载存量策略。
// opts 可注入角色名配置（如 securityauthz.WithRoles(...)）；不传走默认 yudao 口径。
func Init(gdb *gorm.DB, opts ...securityauthz.Option) error {
	a, err := securityauthz.New(gdb, opts...)
	if err != nil {
		return fmt.Errorf("初始化 casbin 失败: %w", err)
	}
	holder.Store(a)
	slog.Info("Casbin enforcer 初始化完成", "table", "casbin_rule")
	return nil
}

// Roles 当前生效的角色名配置（种子、判定、建租户角色行同源取值）。
// 未装配时返回默认配置。
func Roles() securityauthz.RoleConfig {
	if a := loaded(); a != nil {
		return a.Roles()
	}
	return securityauthz.DefaultRoleConfig()
}

// SetAuthorizer 显式注入实例（测试或替代装配路径使用）。
func SetAuthorizer(a *securityauthz.CasbinAuthorizer) { holder.Store(a) }

// Authorizer 导出共享 authorizer（huma 鉴权中间件注入用，见 D7）。
// 未装配时返回 nil，中间件层 fail-closed 拒绝（与 gin 版 Enforce 行为一致）。
func Authorizer() securityauthz.Authorizer {
	if a := loaded(); a != nil {
		return a
	}
	return nil
}

// Close 释放实例（保持装配链对称；共享 SyncedEnforcer 无显式 Close）。
func Close() { holder.Store(nil) }

func loaded() *securityauthz.CasbinAuthorizer { return holder.Load() }

// Enforce 域内鉴权：用户在租户 tid 是否持有 perm。
// 内部错误按拒绝处理并记日志（fail-closed）。
func Enforce(userID, tenantID int64, perm string) bool {
	a := loaded()
	if a == nil {
		return false
	}
	ok, err := a.Enforce(context.Background(),
		strconv.FormatInt(userID, 10), strconv.FormatInt(tenantID, 10), perm)
	if err != nil {
		slog.Error("casbin Enforce 失败", "err", err, "uid", userID, "tid", tenantID, "perm", perm)
		return false
	}
	return ok
}

// EnforcePlatform 平台路由鉴权（dom = "*"，仅 platform_admin 的通配 p 行可匹配）。
func EnforcePlatform(userID int64, perm string) bool {
	a := loaded()
	if a == nil {
		return false
	}
	ok, err := a.EnforcePlatform(context.Background(), strconv.FormatInt(userID, 10), perm)
	if err != nil {
		slog.Error("casbin EnforcePlatform 失败", "err", err, "uid", userID, "perm", perm)
		return false
	}
	return ok
}

// IsPlatformAdmin 用户是否平台管理员（g2 判定，Platform 中间件用）。
func IsPlatformAdmin(userID int64) bool {
	a := loaded()
	if a == nil {
		return false
	}
	ok, err := a.IsPlatformAdmin(context.Background(), strconv.FormatInt(userID, 10))
	if err != nil {
		slog.Error("casbin 查询平台角色失败", "err", err, "uid", userID)
		return false
	}
	return ok
}

// RebuildRolePolicies 按 (role, tenant) 快照重建 p 行：
// 保存角色菜单时先清旧、再把 permission != ” 的行批量加入。
// perms 为空则等价于清空该角色在域内的全部授权。
func RebuildRolePolicies(roleCode string, tenantID int64, perms []string) error {
	a := loaded()
	if a == nil {
		return fmt.Errorf("authz 未初始化")
	}
	return a.RebuildRolePolicies(roleCode, strconv.FormatInt(tenantID, 10), perms)
}

// RemoveRolePolicies 删除角色在域内的全部 p 行（删除角色时调用）。
func RemoveRolePolicies(roleCode string, tenantID int64) error {
	a := loaded()
	if a == nil {
		return fmt.Errorf("authz 未初始化")
	}
	return a.RemoveRolePolicies(roleCode, strconv.FormatInt(tenantID, 10))
}

// RebuildUserGrouping 按 (user, tenant) 重建 g 行（保存用户角色时调用）。
func RebuildUserGrouping(userID, tenantID int64, roleCodes []string) error {
	a := loaded()
	if a == nil {
		return fmt.Errorf("authz 未初始化")
	}
	return a.RebuildUserGrouping(strconv.FormatInt(userID, 10), strconv.FormatInt(tenantID, 10), roleCodes)
}

// RemoveUserGrouping 删除用户在域内的全部 g 行（删除用户时调用）。
func RemoveUserGrouping(userID, tenantID int64) error {
	a := loaded()
	if a == nil {
		return fmt.Errorf("authz 未初始化")
	}
	return a.RemoveUserGrouping(strconv.FormatInt(userID, 10), strconv.FormatInt(tenantID, 10))
}

// RemoveRoleGrouping 删除域内引用某角色的全部 g 行（删除角色时调用）。
// g 三元组按 role 字段过滤。
func RemoveRoleGrouping(roleCode string, tenantID int64) error {
	a := loaded()
	if a == nil {
		return fmt.Errorf("authz 未初始化")
	}
	return a.RemoveRoleGrouping(roleCode, strconv.FormatInt(tenantID, 10))
}

// SeedPolicies 初始策略（幂等，migrate/租户创建复用）：委托共享包 SeedBootstrap，
// 角色名取自 RoleConfig（默认 platform_admin / super_admin）。
//
//	p, {pa}, *, *             平台管理员跨租户全通（g2 路径）
//	g2, {adminUID}, {pa}
//	p, {sa}, {tid}, *         租户超管域内全通
//	g, {adminUID}, {sa}, {tid}
func SeedPolicies(tenantID, adminUID int64) error {
	a := loaded()
	if a == nil {
		return fmt.Errorf("authz 未初始化")
	}
	return a.SeedBootstrap(strconv.FormatInt(adminUID, 10), strconv.FormatInt(tenantID, 10))
}

// SeedTenantAdmin 租户初始超管策略（创建租户时调用，幂等）：
//
//	p, {sa}, {tid}, *         租户超管域内全通
//	g, {adminUID}, {sa}, {tid}
func SeedTenantAdmin(tenantID, adminUID int64) error {
	a := loaded()
	if a == nil {
		return fmt.Errorf("authz 未初始化")
	}
	return a.SeedTenantAdmin(strconv.FormatInt(adminUID, 10), strconv.FormatInt(tenantID, 10))
}
