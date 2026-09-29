// Package middleware 的中间件套件（Kit）：装配一次、各路由模块复用。
//
// 设计：infra 出决策（securitymw/tenantinfra），本包做信封呈现与业务注入（huma.go），
// 业务依赖（DB/缓存/鉴权器/数据源）经 Deps 显式注入——生产装配在 cmd/root.go，
// 测试注入假实现，不再依赖全局单例的直接调用。
package middleware

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"

	"devops-infra/cache"
	securityauthz "devops-infra/security/authz"

	client "devops-infra/database/mysql"
	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/dao"
	"devops-system/internal/model"
	"devops-system/internal/store"
)

// humaMiddleware huma 中间件类型别名（huma 无独立命名类型）。
type humaMiddleware = func(huma.Context, func(huma.Context))

// Deps 业务中间件的依赖集合；nil 字段由 withDefaults 补生产默认绑定。
type Deps struct {
	DB         *gorm.DB                 // 租户可用性查询（全局连接）
	Cache      cache.Cache              // 租户状态缓存；nil 退化为直查
	Authorizer securityauthz.Authorizer // 域内/平台鉴权
	// 数据权限解析数据源（默认绑定 dao；测试可注入假实现）。
	IsPlatformAdmin func(userID int64) bool
	UserRoles        func(ctx context.Context, userID int64) ([]model.Role, error)
	UserByID         func(ctx context.Context, userID int64) (*model.User, error)
	Depts            func(ctx context.Context) ([]model.Dept, error)
}

// withDefaults nil 字段补生产默认绑定（全局单例/门面）。
func (d Deps) withDefaults() Deps {
	if d.DB == nil {
		d.DB = client.Get()
	}
	if d.Cache == nil {
		d.Cache = store.Cache()
	}
	if d.Authorizer == nil {
		d.Authorizer = authz.Authorizer()
	}
	if d.IsPlatformAdmin == nil {
		d.IsPlatformAdmin = authz.IsPlatformAdmin
	}
	if d.UserRoles == nil {
		d.UserRoles = dao.GetUserRoles
	}
	if d.UserByID == nil {
		d.UserByID = dao.GetUserByID
	}
	if d.Depts == nil {
		d.Depts = func(ctx context.Context) ([]model.Dept, error) { return dao.ListDepts(ctx, nil) }
	}
	return d
}

// Kit 业务中间件套件：JWT/租户/鉴权/数据权限中间件的装配与链组合。
type Kit struct {
	jwtM    humaMiddleware
	tenantM humaMiddleware
	scopeM  humaMiddleware
	permM   func(string) humaMiddleware
	platM   func(string) humaMiddleware
	scopes  *scopeCache
}

// NewKit 装配中间件套件；deps 显式注入业务依赖（nil 字段补默认绑定）。
func NewKit(authn *auth.Manager, deps Deps) *Kit {
	deps = deps.withDefaults()
	k := &Kit{
		scopes: newScopeCache(),
	}
	k.jwtM = securityJWT(authn)
	k.tenantM = securityTenant(deps)
	k.scopeM = securityScope(deps, k.scopes)
	k.permM = func(perm string) humaMiddleware { return securityPerm(deps.Authorizer, perm) }
	k.platM = func(perm string) humaMiddleware { return securityPlat(deps.Authorizer, perm) }
	return k
}

// JWT 认证中间件（Bearer → token 解析 → 会话态 → claims 注入 ctx）。
func (k *Kit) JWT() humaMiddleware { return k.jwtM }

// Tenant 租户校验 + 上下文注入（可用性判定带 Redis 缓存）。
func (k *Kit) Tenant() humaMiddleware { return k.tenantM }

// Scope 行级数据权限解析中间件（受控资源路由用）。
func (k *Kit) Scope() humaMiddleware { return k.scopeM }

// Perm 域内鉴权中间件（uid × tid × perm）。
func (k *Kit) Perm(perm string) humaMiddleware { return k.permM(perm) }

// Plat 平台鉴权中间件（平台管理员）。
func (k *Kit) Plat(perm string) humaMiddleware { return k.platM(perm) }

// Logged 仅认证链（登出等无租户语义的接口）。
func (k *Kit) Logged() huma.Middlewares { return huma.Middlewares{k.jwtM} }

// TenantBiz 认证 + 租户链（auth 模块个人信息类接口）。
func (k *Kit) TenantBiz() huma.Middlewares { return huma.Middlewares{k.jwtM, k.tenantM} }

// Biz 业务路由中间件链：JWT → Tenant → Perm(域内)。
func (k *Kit) Biz(perm string) huma.Middlewares {
	return huma.Middlewares{k.jwtM, k.tenantM, k.permM(perm)}
}

// ScopedBiz 受控资源路由链：JWT → Tenant → Perm → DataScope（行级数据权限解析）。
// 只挂在需要行级过滤的资源（users）；非受控资源不加，保持解析开销与语义边界显式。
func (k *Kit) ScopedBiz(perm string) huma.Middlewares {
	return huma.Middlewares{k.jwtM, k.tenantM, k.permM(perm), k.scopeM}
}

// PlatChain 平台路由中间件链：JWT → PlatformPerm（不挂 Tenant，操作全局表）。
func (k *Kit) PlatChain(perm string) huma.Middlewares {
	return huma.Middlewares{k.jwtM, k.platM(perm)}
}

// InvalidateDataScope 清空数据权限缓存（角色保存/删除、用户角色变更时调用）。
func (k *Kit) InvalidateDataScope() {
	k.scopes.invalidate()
}
