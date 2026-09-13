// Package authz 基于 Casbin 域模型的权限鉴权。
//
// 模型要点：
//   - sub = user_id、dom = tenant_id、obj = 权限标识（如 system:user:create）
//   - g(user, role, tenant) 三元组：租户内角色，user_id 在不同租户是不同 g 行，天然隔离
//   - g2(user, platform_admin) 二元组：平台角色（不绑域）——域内 * 与平台能力彻底分离
//   - p(role, tenant, perm)：角色在某租户的权限快照；p, platform_admin, *, * 平台管理员跨域全通
//
// 防越权边界（6.3）：platform:tenant:* 权限标识只出现在平台路由；
// 租户超管的 p, super_admin, {tid}, * 因 dom 不匹配 "*" 无法触达平台接口。
package authz

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

// domainModel Casbin 域模型（内嵌常量）。
const domainModel = `
[request_definition]
r = sub, dom, obj

[policy_definition]
p = sub, dom, obj

[role_definition]
g = _, _, _
g2 = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = (g(r.sub, p.sub, r.dom) || g2(r.sub, p.sub)) && (r.dom == p.dom || p.dom == "*") && keyMatch(r.obj, p.obj)
`

var e *casbin.SyncedEnforcer

// Init 装配 enforcer：gorm-adapter 持久化（casbin_rule 表）+ 加载存量策略。
func Init(gdb *gorm.DB) error {
	a, err := gormadapter.NewAdapterByDB(gdb)
	if err != nil {
		return fmt.Errorf("初始化 casbin adapter 失败: %w", err)
	}
	m, err := model.NewModelFromString(domainModel)
	if err != nil {
		return fmt.Errorf("解析 casbin 域模型失败: %w", err)
	}
	e, err = casbin.NewSyncedEnforcer(m, a)
	if err != nil {
		return fmt.Errorf("初始化 casbin enforcer 失败: %w", err)
	}
	if err := e.LoadPolicy(); err != nil {
		return fmt.Errorf("加载 casbin 策略失败: %w", err)
	}
	slog.Info("Casbin enforcer 初始化完成", "table", "casbin_rule")
	return nil
}

// Close 释放 enforcer（v3 SyncedEnforcer 无显式 Close，保留接口以对称装配链）。
func Close() {}

// Enforce 域内鉴权：用户在租户 tid 是否持有 perm。
// 线程安全（SyncedEnforcer）；内部错误按拒绝处理并记日志（fail-closed）。
func Enforce(userID, tenantID int64, perm string) bool {
	if e == nil {
		return false
	}
	ok, err := e.Enforce(strconv.FormatInt(userID, 10), strconv.FormatInt(tenantID, 10), perm)
	if err != nil {
		slog.Error("casbin Enforce 失败", "err", err, "uid", userID, "tid", tenantID, "perm", perm)
		return false
	}
	return ok
}

// EnforcePlatform 平台路由鉴权（dom = "*"，仅 platform_admin 的通配 p 行可匹配）。
func EnforcePlatform(userID int64, perm string) bool {
	if e == nil {
		return false
	}
	ok, err := e.Enforce(strconv.FormatInt(userID, 10), "*", perm)
	if err != nil {
		slog.Error("casbin EnforcePlatform 失败", "err", err, "uid", userID, "perm", perm)
		return false
	}
	return ok
}

// IsPlatformAdmin 用户是否平台管理员（g2 判定，Platform 中间件用）。
func IsPlatformAdmin(userID int64) bool {
	if e == nil {
		return false
	}
	uid := strconv.FormatInt(userID, 10)
	ok, err := e.HasNamedGroupingPolicy("g2", uid, "platform_admin")
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
	if e == nil {
		return fmt.Errorf("authz 未初始化")
	}
	dom := strconv.FormatInt(tenantID, 10)
	if _, err := e.RemoveFilteredPolicy(0, roleCode, dom); err != nil {
		return fmt.Errorf("清理角色策略失败(%s,%d): %w", roleCode, tenantID, err)
	}
	if len(perms) == 0 {
		return nil
	}
	rules := make([][]string, 0, len(perms))
	for _, p := range perms {
		rules = append(rules, []string{roleCode, dom, p})
	}
	if _, err := e.AddPolicies(rules); err != nil {
		return fmt.Errorf("写入角色策略失败(%s,%d): %w", roleCode, tenantID, err)
	}
	return nil
}

// RemoveRolePolicies 删除角色在域内的全部 p 行（删除角色时调用）。
func RemoveRolePolicies(roleCode string, tenantID int64) error {
	if e == nil {
		return fmt.Errorf("authz 未初始化")
	}
	dom := strconv.FormatInt(tenantID, 10)
	if _, err := e.RemoveFilteredPolicy(0, roleCode, dom); err != nil {
		return fmt.Errorf("删除角色策略失败(%s,%d): %w", roleCode, tenantID, err)
	}
	return nil
}

// RebuildUserGrouping 按 (user, tenant) 重建 g 行（保存用户角色时调用）。
func RebuildUserGrouping(userID, tenantID int64, roleCodes []string) error {
	if e == nil {
		return fmt.Errorf("authz 未初始化")
	}
	uid := strconv.FormatInt(userID, 10)
	dom := strconv.FormatInt(tenantID, 10)
	if _, err := e.RemoveFilteredNamedGroupingPolicy("g", 0, uid, "", dom); err != nil {
		return fmt.Errorf("清理用户角色失败(uid=%d,tid=%d): %w", userID, tenantID, err)
	}
	if len(roleCodes) == 0 {
		return nil
	}
	rules := make([][]string, 0, len(roleCodes))
	for _, code := range roleCodes {
		rules = append(rules, []string{uid, code, dom})
	}
	if _, err := e.AddNamedGroupingPolicies("g", rules); err != nil {
		return fmt.Errorf("写入用户角色失败(uid=%d,tid=%d): %w", userID, tenantID, err)
	}
	return nil
}

// RemoveUserGrouping 删除用户在域内的全部 g 行（删除用户时调用）。
func RemoveUserGrouping(userID, tenantID int64) error {
	if e == nil {
		return fmt.Errorf("authz 未初始化")
	}
	uid := strconv.FormatInt(userID, 10)
	dom := strconv.FormatInt(tenantID, 10)
	if _, err := e.RemoveFilteredNamedGroupingPolicy("g", 0, uid, "", dom); err != nil {
		return fmt.Errorf("删除用户角色失败(uid=%d,tid=%d): %w", userID, tenantID, err)
	}
	return nil
}

// RemoveRoleGrouping 删除域内引用某角色的全部 g 行（删除角色时调用）。
// g 三元组按 role 字段（fieldIndex=1）过滤。
func RemoveRoleGrouping(roleCode string, tenantID int64) error {
	if e == nil {
		return fmt.Errorf("authz 未初始化")
	}
	dom := strconv.FormatInt(tenantID, 10)
	if _, err := e.RemoveFilteredNamedGroupingPolicy("g", 1, roleCode, dom); err != nil {
		return fmt.Errorf("清理角色用户绑定失败(%s,%d): %w", roleCode, tenantID, err)
	}
	return nil
}

// SeedPolicies 初始策略（幂等，migrate/租户创建复用）：
//
//	p, platform_admin, *, *   平台管理员跨租户全通（g2 路径）
//	g2, {adminUID}, platform_admin
//	p, super_admin, {tid}, *  租户超管域内全通
//	g, {adminUID}, super_admin, {tid}
func SeedPolicies(tenantID, adminUID int64) error {
	if e == nil {
		return fmt.Errorf("authz 未初始化")
	}
	tid := strconv.FormatInt(tenantID, 10)
	uid := strconv.FormatInt(adminUID, 10)

	type rule struct {
		ptype string
		vals  []string
	}
	for _, r := range []rule{
		{"p", []string{"platform_admin", "*", "*"}},
		{"g2", []string{uid, "platform_admin"}},
		{"p", []string{"super_admin", tid, "*"}},
		{"g", []string{uid, "super_admin", tid}},
	} {
		switch r.ptype {
		case "p":
			has, err := e.HasPolicy(toAny(r.vals)...)
			if err != nil {
				return err
			}
			if !has {
				if _, err := e.AddPolicy(toAny(r.vals)...); err != nil {
					return fmt.Errorf("写入种子 p 行失败: %w", err)
				}
			}
		case "g":
			has, err := e.HasNamedGroupingPolicy("g", toAny(r.vals)...)
			if err != nil {
				return err
			}
			if !has {
				if _, err := e.AddNamedGroupingPolicy("g", toAny(r.vals)...); err != nil {
					return fmt.Errorf("写入种子 g 行失败: %w", err)
				}
			}
		case "g2":
			has, err := e.HasNamedGroupingPolicy("g2", toAny(r.vals)...)
			if err != nil {
				return err
			}
			if !has {
				if _, err := e.AddNamedGroupingPolicy("g2", toAny(r.vals)...); err != nil {
					return fmt.Errorf("写入种子 g2 行失败: %w", err)
				}
			}
		}
	}
	return nil
}

// SeedTenantAdmin 租户初始超管策略（创建租户时调用，幂等）：
//
//	p, super_admin, {tid}, *   租户超管域内全通
//	g, {adminUID}, super_admin, {tid}
func SeedTenantAdmin(tenantID, adminUID int64) error {
	if e == nil {
		return fmt.Errorf("authz 未初始化")
	}
	tid := strconv.FormatInt(tenantID, 10)
	uid := strconv.FormatInt(adminUID, 10)

	pRule := []any{"super_admin", tid, "*"}
	has, err := e.HasPolicy(pRule...)
	if err != nil {
		return err
	}
	if !has {
		if _, err := e.AddPolicy(pRule...); err != nil {
			return fmt.Errorf("写入租户超管策略失败: %w", err)
		}
	}
	gRule := []any{uid, "super_admin", tid}
	has, err = e.HasNamedGroupingPolicy("g", gRule...)
	if err != nil {
		return err
	}
	if !has {
		if _, err := e.AddNamedGroupingPolicy("g", gRule...); err != nil {
			return fmt.Errorf("写入租户超管绑定失败: %w", err)
		}
	}
	return nil
}

// toAny []string → []any（适配 v3 的 ...interface{} 签名）。
func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
