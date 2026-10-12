package authz

import (
	"context"
	"errors"
	"log/slog"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

var ErrUninitialized = errors.New("authz: authorizer is not initialized")

type Authorizer interface {
	Enforce(ctx context.Context, subject, domain, permission string) (bool, error)
	EnforcePlatform(ctx context.Context, subject, permission string) (bool, error)
	IsPlatformAdmin(ctx context.Context, subject string) (bool, error)
}

type PolicyManager interface {
	RebuildRolePolicies(role, domain string, permissions []string) error
	RemoveRolePolicies(role, domain string) error
	RebuildUserGrouping(subject, domain string, roles []string) error
	RemoveUserGrouping(subject, domain string) error
	RemoveRoleGrouping(role, domain string) error
	AddPolicyIfAbsent(policyType string, values ...string) error
}

type CasbinAuthorizer struct {
	enforcer *casbin.SyncedEnforcer
	roles    RoleConfig
}

// Option 构造期可选配置。
type Option func(*options)

type options struct {
	roles RoleConfig
}

// WithRoles 自定义角色名（零值字段回填默认）。
func WithRoles(c RoleConfig) Option {
	return func(o *options) { o.roles = c }
}

func New(gdb *gorm.DB, opts ...Option) (*CasbinAuthorizer, error) {
	if gdb == nil {
		return nil, ErrUninitialized
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}
	roles := o.roles.withDefaults()

	adapter, err := gormadapter.NewAdapterByDB(gdb)
	if err != nil {
		return nil, err
	}

	m, err := newModel()
	if err != nil {
		return nil, err
	}

	enforcer, err := casbin.NewSyncedEnforcer(m, adapter)
	if err != nil {
		return nil, err
	}
	if err := enforcer.LoadPolicy(); err != nil {
		return nil, err
	}

	return &CasbinAuthorizer{enforcer: enforcer, roles: roles}, nil
}

// Roles 当前生效的角色名配置（种子与判定同源）。
func (a *CasbinAuthorizer) Roles() RoleConfig {
	if a == nil {
		return DefaultRoleConfig()
	}
	return a.roles
}

func (a *CasbinAuthorizer) Enforce(ctx context.Context, subject, domain, permission string) (bool, error) {
	_ = ctx

	enforcer, err := a.getEnforcer()
	if err != nil {
		return false, err
	}

	ok, err := enforcer.Enforce(subject, domain, permission)
	if err != nil {
		slog.Error("casbin enforce failed", "subject", subject, "domain", domain, "permission", permission, "err", err)
		return false, err
	}
	return ok, nil
}

func (a *CasbinAuthorizer) EnforcePlatform(ctx context.Context, subject, permission string) (bool, error) {
	return a.Enforce(ctx, subject, "*", permission)
}

func (a *CasbinAuthorizer) IsPlatformAdmin(ctx context.Context, subject string) (bool, error) {
	_ = ctx

	enforcer, err := a.getEnforcer()
	if err != nil {
		return false, err
	}

	ok, err := enforcer.HasNamedGroupingPolicy("g2", subject, a.roles.PlatformAdmin)
	if err != nil {
		slog.Error("casbin platform-admin check failed", "subject", subject, "err", err)
		return false, err
	}
	return ok, nil
}

func (a *CasbinAuthorizer) getEnforcer() (*casbin.SyncedEnforcer, error) {
	if a == nil || a.enforcer == nil {
		return nil, ErrUninitialized
	}
	return a.enforcer, nil
}
