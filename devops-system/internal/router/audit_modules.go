package router

import (
	"github.com/stanley760/devops/devops-infra/security/humakit"
)

// 审计模块名（写入 t_system_operation_log 的 module / sub_module 两列）。
const (
	auditModuleSystem = "system"
)

// auditedOperations 声明纳入操作审计的路由（OperationID → 子模块）。
//
// 收录原则：
//   - 只审计写操作（POST/PUT/DELETE）与敏感读操作；
//     GET 列表/详情不审计，避免高频轮询造成审计表膨胀。
//   - 登录审计由 t_system_login_log 独立承载（记录成功与失败两次尝试，
//     含未通过认证的场景），故 auth-login 不在此重复登记。
//   - 审计表自身的查询与清理不审计（自审计无意义且会放大写入）。
var auditedOperations = map[string]string{
	// 用户
	"user-create":         "user",
	"user-update":         "user",
	"user-delete":         "user",
	"user-reset-password": "user",
	// 角色
	"role-create":       "role",
	"role-update":       "role",
	"role-delete":       "role",
	"role-assign-menus": "role",
	// 菜单
	"menu-create": "menu",
	"menu-update": "menu",
	"menu-delete": "menu",
	// 部门
	"dept-create": "dept",
	"dept-update": "dept",
	"dept-delete": "dept",
	// 岗位
	"post-create": "post",
	"post-update": "post",
	"post-delete": "post",
	// 租户（平台级）
	"tenant-create":        "tenant",
	"tenant-update":        "tenant",
	"tenant-delete":        "tenant",
	"tenant-update-status": "tenant",
	// 个人账号
	"auth-update-profile":  "profile",
	"auth-update-password": "profile",
}

// auditModuleResolver 把 OperationID 解析为审计模块。
//
// 装配点（cmd 层）经 humakit.EnableAudit 传入本函数：审计中间件按此白名单
// 决定哪些路由落库。新增写操作时在此登记一行即可，无需改动 handler。
func auditModuleResolver(operationID string) (module, subModule string, ok bool) {
	subModule, ok = auditedOperations[operationID]
	if !ok {
		return "", "", false
	}
	return auditModuleSystem, subModule, true
}

// AuditModuleResolver 导出给 cmd 装配层：
// router 包持有"哪些路由该审计"这张表，cmd 只负责注入 Sink，两边职责不混。
func AuditModuleResolver() humakit.ModuleResolver { return auditModuleResolver }

// 编译期断言：本模块实现的解析器满足 infra 侧契约。
var _ humakit.ModuleResolver = auditModuleResolver
