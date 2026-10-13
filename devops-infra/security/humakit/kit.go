// Package humakit 业务中间件套件：JWT 认证 → 租户 → 域内/平台鉴权 → 数据权限解析
// 的装配与链组合。
//
// 分层：决策在 security/middleware 与 tenant（infra 决策原语），本包做链组合、
// 数据权限解析与 fail-closed 收口；文案呈现与业务数据源一律经 Deps/Envelope 注入。
// 这样各业务模块（devops-system、devops-user）可各自装配自己的文案与数据源，
// 而不复制中间件链实现。
package humakit

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/stanley760/devOps/devops-infra/datascope"
	"github.com/stanley760/devOps/devops-infra/http/response"
	securityauthz "github.com/stanley760/devOps/devops-infra/security/authz"
	securityjwt "github.com/stanley760/devOps/devops-infra/security/jwt"
	securitymw "github.com/stanley760/devOps/devops-infra/security/middleware"
)

// Middleware huma 中间件类型别名（huma 无独立命名类型）。
type Middleware = func(huma.Context, func(huma.Context))

// Envelope 错误呈现回调（文案属业务，经此注入）。
// nil 字段走安全默认（HTTP 状态短语信封），避免漏注入时返回无 body 的裸状态码。
type Envelope struct {
	Unauthorized      func(hctx huma.Context, err error) // 401 基础（含会话失效文案映射）
	AuthnInfra        func(hctx huma.Context, err error) // 401/503 认证基础设施错误
	TenantForbidden   func(hctx huma.Context, err error) // 403 租户不可用
	PermForbidden     func(hctx huma.Context, err error) // 403 域内无权限
	PlatformForbidden func(hctx huma.Context, err error) // 403 平台（可区分非管理员文案）
	AuthzInfra        func(hctx huma.Context, err error) // 403/503 鉴权基础设施错误
}

// Deps 业务依赖全部经回调/接口注入；humakit 不做默认业务绑定（装配属业务侧）。
// nil 语义一律 fail-closed：TenantUsable nil→恒 false；Authorizer nil→拒绝；
// 解析数据源 nil→解析报错→空 Scope（不可见任何行）。
type Deps struct {
	Authorizer securityauthz.Authorizer // 域内/平台鉴权；nil → 拒绝
	// SuperAdminRole 租户超管角色 code（数据权限全域直通判定）；
	// "" → security/authz.DefaultRoleConfig().SuperAdmin。
	SuperAdminRole string
	// ScopeTTL 数据权限解析结果缓存时长；0 → 60s。
	ScopeTTL time.Duration
	// TenantUsable 租户可用性判定（停用/过期/不存在 → false）；nil → 恒 false。
	TenantUsable func(ctx context.Context, tenantID int64) bool
	// IsPlatformAdmin 平台管理员判定（数据权限全域直通用）；nil → false。
	IsPlatformAdmin func(ctx context.Context, userID int64) bool
	// 数据权限解析数据源；任一为 nil → 解析失败 → 空 Scope。
	UserRoles func(ctx context.Context, userID int64) ([]datascope.RoleScope, error)
	User      func(ctx context.Context, userID int64) (datascope.UserRef, error)
	Depts     func(ctx context.Context) ([]datascope.DeptNode, error)
	// WithClaims 认证成功后的 ctx 增强（审计 actor 等）；nil → 原样透传。
	WithClaims func(ctx context.Context, claims *securityjwt.Claims) context.Context
	// Envelope 错误呈现回调；nil 字段走安全默认。
	Envelope Envelope
}

const defaultScopeTTL = 60 * time.Second

// withDefaults 零值字段回填安全默认（不做业务绑定）。
func (d Deps) withDefaults() Deps {
	if d.SuperAdminRole == "" {
		// 与 security/authz 的 RoleConfig 同源，避免两处默认值漂移。
		d.SuperAdminRole = securityauthz.DefaultRoleConfig().SuperAdmin
	}
	if d.ScopeTTL <= 0 {
		d.ScopeTTL = defaultScopeTTL
	}
	if d.TenantUsable == nil {
		d.TenantUsable = func(context.Context, int64) bool { return false }
	}
	if d.IsPlatformAdmin == nil {
		d.IsPlatformAdmin = func(context.Context, int64) bool { return false }
	}
	d.Envelope = d.Envelope.withDefaults()
	return d
}

// withDefaults nil 槽位回退到 HTTP 状态短语信封（与 securitymw.withHumaDefaults 同语义）。
func (e Envelope) withDefaults() Envelope {
	if e.Unauthorized == nil {
		e.Unauthorized = statusPhrase(http.StatusUnauthorized)
	}
	if e.AuthnInfra == nil {
		e.AuthnInfra = statusPhrase(http.StatusServiceUnavailable)
	}
	if e.TenantForbidden == nil {
		e.TenantForbidden = statusPhrase(http.StatusForbidden)
	}
	if e.PermForbidden == nil {
		e.PermForbidden = statusPhrase(http.StatusForbidden)
	}
	if e.PlatformForbidden == nil {
		e.PlatformForbidden = statusPhrase(http.StatusForbidden)
	}
	if e.AuthzInfra == nil {
		e.AuthzInfra = statusPhrase(http.StatusServiceUnavailable)
	}
	return e
}

// statusPhrase 固定状态码 + HTTP 状态短语的信封回调。
func statusPhrase(status int) func(huma.Context, error) {
	return func(hctx huma.Context, _ error) {
		response.WriteHumaEnvelope(hctx, status, http.StatusText(status))
	}
}

// Kit 业务中间件套件：JWT/租户/鉴权/数据权限中间件的装配与链组合。
type Kit struct {
	authn   securitymw.BearerAuthenticator
	deps    Deps
	jwtM    Middleware
	tenantM Middleware
	scopeM  Middleware
	permM   func(string) Middleware
	platM   func(string) Middleware
	scopes  *scopeCache
}

// NewKit 装配中间件套件；deps 的 nil/零值项按 fail-closed 语义回填安全默认。
func NewKit(authn securitymw.BearerAuthenticator, deps Deps) *Kit {
	deps = deps.withDefaults()
	k := &Kit{
		authn:  authn,
		deps:   deps,
		scopes: newScopeCache(deps.ScopeTTL),
	}
	k.jwtM = k.securityJWT()
	k.tenantM = k.securityTenant()
	k.scopeM = k.securityScope()
	k.permM = func(perm string) Middleware { return k.securityPerm(perm) }
	k.platM = func(perm string) Middleware { return k.securityPlat(perm) }
	return k
}

// JWT 认证中间件（Bearer → token 解析 → 会话态 → claims 注入 ctx）。
func (k *Kit) JWT() Middleware { return k.jwtM }

// Tenant 租户校验 + 上下文注入。
func (k *Kit) Tenant() Middleware { return k.tenantM }

// Scope 行级数据权限解析中间件（受控资源路由用）。
func (k *Kit) Scope() Middleware { return k.scopeM }

// Perm 域内鉴权中间件（uid × tid × perm）。
func (k *Kit) Perm(perm string) Middleware { return k.permM(perm) }

// Plat 平台鉴权中间件（平台管理员）。
func (k *Kit) Plat(perm string) Middleware { return k.platM(perm) }

// Logged 仅认证链（登出等无租户语义的接口）。
func (k *Kit) Logged() huma.Middlewares { return huma.Middlewares{k.jwtM} }

// TenantBiz 认证 + 租户链（个人信息类接口）。
func (k *Kit) TenantBiz() huma.Middlewares { return huma.Middlewares{k.jwtM, k.tenantM} }

// Biz 业务路由中间件链：JWT → Tenant → Perm(域内)。
func (k *Kit) Biz(perm string) huma.Middlewares {
	return huma.Middlewares{k.jwtM, k.tenantM, k.permM(perm)}
}

// ScopedBiz 受控资源路由链：JWT → Tenant → Perm → DataScope（行级数据权限解析）。
// 只挂在需要行级过滤的资源；非受控资源不加，保持解析开销与语义边界显式。
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
