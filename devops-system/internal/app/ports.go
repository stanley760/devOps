// Package app 是应用装配层（Composition Root）。
//
// 依赖方向总览（DDD 的依赖倒置在此落地）：
//
//	cmd ──→ app ──→ application ──→ domain ←── infrastructure
//	              │         （端口接口）      （GORM/Casbin/Redis 实现）
//	              └──→ api/v1、router、middleware
//
// 本包是**唯一**允许同时认识「接口」与「实现」的地方：
// 它把 infrastructure 里的 GORM 仓储实现注入 application 声明的端口。
// domain 与 application 包内不存在任何对 infrastructure 的 import，
// 故领域规则可脱离数据库与 HTTP 独立测试。
package app

import (
	"github.com/stanley760/devops/devops-system/internal/application/command"
	"github.com/stanley760/devops/devops-system/internal/application/query"
	"github.com/stanley760/devops/devops-system/internal/auth"
	"github.com/stanley760/devops/devops-system/internal/domain/auditlog"
	"github.com/stanley760/devops/devops-system/internal/domain/iam"
	"github.com/stanley760/devops/devops-system/internal/domain/shared"
	"github.com/stanley760/devops/devops-system/internal/infrastructure/event"
	"github.com/stanley760/devops/devops-system/internal/infrastructure/persistence"
)

// Repositories 汇总全部仓储实现（由本包注入各端口）。
type Repositories struct {
	Users   iam.UserRepository
	Roles   iam.RoleRepository
	Menus   iam.MenuRepository
	Depts   iam.DeptRepository
	Posts   iam.PostRepository
	Tenants iam.TenantRepository

	LoginLogs     auditlog.LoginLogReader
	LoginLogWrite auditlog.LoginLogWriter
	OperationLogs auditlog.OperationLogReader
}

// NewRepositories 构造全部仓储实现（GORM）。
func NewRepositories() Repositories {
	return Repositories{
		Users:         persistence.NewUserRepository(),
		Roles:         persistence.NewRoleRepository(),
		Menus:         persistence.NewMenuRepository(),
		Depts:         persistence.NewDeptRepository(),
		Posts:         persistence.NewPostRepository(),
		Tenants:       persistence.NewTenantRepository(),
		LoginLogs:     persistence.NewLoginLogReader(),
		LoginLogWrite: persistence.NewLoginLogRepository(),
		OperationLogs: persistence.NewOperationLogReader(),
	}
}

// Commands 汇总全部写侧命令处理器。
type Commands struct {
	User   *command.UserCommand
	Role   *command.RoleCommand
	Org    *command.OrgCommand
	Tenant *command.TenantCommand
}

// Queries 汇总全部读侧查询处理器。
type Queries struct {
	User         *query.UserQuery
	Role         *query.RoleQuery
	Menu         *query.MenuQuery
	Dept         *query.DeptQuery
	Post         *query.PostQuery
	Tenant       *query.TenantQuery
	LoginLog     *query.LoginLogQuery
	OperationLog *query.OperationLogQuery
}

// NewCommands 构造写侧处理器（注入仓储端口 + 事务管理器）。
func NewCommands(repos Repositories, txs shared.TxManager) Commands {
	return Commands{
		User:   command.NewUserCommand(repos.Users, txs),
		Role:   command.NewRoleCommand(repos.Roles, repos.Menus, txs),
		Org:    command.NewOrgCommand(repos.Depts, repos.Posts, repos.Menus, txs),
		Tenant: command.NewTenantCommand(repos.Tenants, repos.Users, repos.Roles, txs),
	}
}

// NewQueries 构造读侧处理器（注入仓储端口）。
func NewQueries(repos Repositories) Queries {
	return Queries{
		User:         query.NewUserQuery(repos.Users),
		Role:         query.NewRoleQuery(repos.Roles),
		Menu:         query.NewMenuQuery(repos.Menus),
		Dept:         query.NewDeptQuery(repos.Depts),
		Post:         query.NewPostQuery(repos.Posts),
		Tenant:       query.NewTenantQuery(repos.Tenants),
		LoginLog:     query.NewLoginLogQuery(repos.LoginLogs),
		OperationLog: query.NewOperationLogQuery(repos.OperationLogs),
	}
}

// NewTxManager 构造事务管理器（GORM 实现）。
func NewTxManager() shared.TxManager { return persistence.NewTxManager() }

// EventDispatch 事件分发所需组件。
type EventDispatch struct {
	Publisher shared.EventPublisher
	Store     shared.EventStore
}

// NewEventDispatch 构造事件分发器与投影器。
//
// invalidateScope 为数据权限缓存失效回调：由 middleware.Kit 提供，
// 但注入方向是 app → 投影器，而非改造前 service → kit.InvalidateDataScope
// （后者是「应用层反向依赖中间件层」）。
//
// 改造前 app/service_providers.go 有两个手写 provider，专门把
// kit.InvalidateDataScope 包成 service 能吃的形态；现在这条反向依赖消失，
// wire provider 集合也随之简化。
func NewEventDispatch(authM *auth.Manager, invalidateScope func()) EventDispatch {
	return EventDispatch{
		Publisher: persistence.NewDispatcher(
			event.NewCasbinProjector(),
			event.NewSessionProjector(authM),
			event.NewScopeCacheProjector(invalidateScope),
		),
		Store: persistence.NewEventStore(),
	}
}
