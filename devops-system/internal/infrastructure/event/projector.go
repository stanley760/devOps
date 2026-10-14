// Package event 是领域事件的投影器实现（把写模型变更同步到 casbin 读模型）。
//
// 这是 CQRS 中「写模型 / 读模型分离」的落地：聚合在事务内记录领域事件，
// 授权策略的同步由投影器完成，投影失败不影响主流程响应，且因事件已持久化
// 可重试而不会丢失。
//
// 改造前的问题（3 处手工补偿）：
//
//	service/user.go:130  "用户已创建，但角色授权写入失败，请重试更新"
//	service/user.go:195  "用户已更新，但角色授权刷新失败，请重试更新"
//	service/user.go:250  "用户已删除，但角色授权清理失败"
//	service/role.go:168  "菜单已保存，但权限策略刷新失败，请重试"
//
// 这些错误的共同点是：业务行已提交、授权未生效，客户端拿到一个语义模糊的
// 提示，既不知道该重试还是该人工介入，也无法从数据上判断授权是否已同步。
// 现在这些场景统一为：主流程返回成功 + 事件入表 + 投影器同步执行，
// 失败则事件保持待投影状态并由重试任务补偿。
package event

import (
	"context"
	"log/slog"

	"github.com/stanley760/devops/devops-system/internal/auth"
	"github.com/stanley760/devops/devops-system/internal/authz"
	"github.com/stanley760/devops/devops-system/internal/domain/iam"
	"github.com/stanley760/devops/devops-system/internal/domain/shared"
)

// casbinProjector 把 IAM 领域事件投影到 casbin 策略。
//
// 幂等性：casbin 的 RebuildXxx 系列均为「先清后建」，RemoveXxx 为条件删除，
// 重复执行结果一致，满足至少一次投递下的幂等要求。
type casbinProjector struct{}

// NewCasbinProjector 构造 casbin 投影器。
func NewCasbinProjector() shared.EventProjector { return casbinProjector{} }

// Name 实现命名（供分发器日志使用）。
func (casbinProjector) Name() string { return "casbin" }

// Handles 声明订阅的事件。
func (casbinProjector) Handles() []string {
	return []string{
		iam.EventUserRolesAssigned,
		iam.EventUserDeactivated,
		iam.EventUserDeleted,
		iam.EventRoleMenusAssigned,
		iam.EventRoleDeleted,
		iam.EventTenantAdminSeeded,
	}
}

// Handle 分发单个事件到对应的同步动作。
func (casbinProjector) Handle(ctx context.Context, e shared.Event) error {
	switch ev := e.(type) {
	case iam.UserRolesAssigned:
		return authz.RebuildUserGrouping(ev.UserID, ev.TenantID, ev.RoleCodes)
	case iam.UserDeactivated:
		return nil // 会话撤销由 sessionProjector 处理
	case iam.UserDeleted:
		return authz.RemoveUserGrouping(ev.UserID, ev.TenantID)
	case iam.RoleMenusAssigned:
		return authz.RebuildRolePolicies(ev.RoleCode, ev.TenantID, ev.Permissions)
	case iam.RoleDeleted:
		if err := authz.RemoveRolePolicies(ev.RoleCode, ev.TenantID); err != nil {
			return err
		}
		return authz.RemoveRoleGrouping(ev.RoleCode, ev.TenantID)
	case iam.TenantAdminSeeded:
		// 新租户初始超管：域内全通 p 行 + g 行绑定（既有 SeedTenantAdmin 的语义）。
		// 幂等：p/g 写入走 casbin adapter 的 add，重复执行不产生重复行。
		return authz.SeedTenantAdmin(ev.TenantID, ev.AdminUserID)
	default:
		slog.WarnContext(ctx, "casbin 投影器收到未知事件", "event", e.EventName())
		return nil
	}
}

// sessionProjector 把「用户/租户不可用」事件投影为会话撤销（踢下线）。
type sessionProjector struct {
	authM *auth.Manager
}

// NewSessionProjector 构造会话投影器。
func NewSessionProjector(authM *auth.Manager) shared.EventProjector {
	return sessionProjector{authM: authM}
}

// Name 实现命名。
func (sessionProjector) Name() string { return "session" }

// Handles 声明订阅的事件。
func (sessionProjector) Handles() []string {
	return []string{iam.EventUserDeactivated, iam.EventTenantDisabled}
}

// Handle 撤销对应范围的会话。
func (p sessionProjector) Handle(_ context.Context, e shared.Event) error {
	if p.authM == nil {
		slog.Error("会话投影器未装配 auth.Manager，跳过会话撤销", "event", e.EventName())
		return nil
	}
	switch ev := e.(type) {
	case iam.UserDeactivated:
		p.authM.RevokeAllSessions(context.Background(), ev.TenantID, ev.UserID)
	case iam.TenantDisabled:
		p.authM.RevokeTenantSessions(context.Background(), ev.TenantID)
	}
	return nil
}

// cacheProjector 清理数据权限缓存。
//
// 改造前是 service/user.go:199 与 service/role.go:139/195 的
// invalidateScope() 直接调用——应用层因此依赖了中间件层的回调
// （app/service_providers.go 把 kit.InvalidateDataScope 传给 service，
// 是「service 依赖 middleware」的反向设计）。
// 现改为事件驱动：应用层不再持有中间件回调，依赖方向摆正。
type cacheProjector struct {
	invalidate func()
}

// NewScopeCacheProjector 构造数据权限缓存投影器。
func NewScopeCacheProjector(invalidate func()) shared.EventProjector {
	return cacheProjector{invalidate: invalidate}
}

// Name 实现命名。
func (cacheProjector) Name() string { return "scope-cache" }

// Handles 声明订阅的事件：角色分配与数据权限口径变化都会改变可见范围。
func (cacheProjector) Handles() []string {
	return []string{iam.EventUserRolesAssigned, iam.EventRoleMenusAssigned, iam.EventRoleDeleted}
}

// Handle 清理缓存。
func (p cacheProjector) Handle(_ context.Context, _ shared.Event) error {
	if p.invalidate != nil {
		p.invalidate()
	}
	return nil
}
