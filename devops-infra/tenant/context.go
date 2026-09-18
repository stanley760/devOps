// Package tenant 提供多租户请求上下文和数据库隔离辅助能力。
package tenant

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrTenantNotFound 表示当前执行上下文没有租户编号。
	ErrTenantNotFound = errors.New("tenant: tenant id is not set")
	// ErrInvalidTenantID 表示租户编号不是正数。
	ErrInvalidTenantID = errors.New("tenant: tenant id must be greater than zero")
	// ErrContextMissing 表示 GORM 租户插件未找到租户上下文。
	ErrContextMissing = errors.New("tenant: context is missing")
)

type contextKey struct{}

type Context struct {
	ID   int64
	Code string
}

type contextState struct {
	tenant Context
	skip   bool
}

// With 返回带有租户编号和租户编码的上下文。
func With(ctx context.Context, tenantID int64, tenantCode string) context.Context {
	return context.WithValue(ctx, contextKey{}, contextState{tenant: Context{ID: tenantID, Code: tenantCode}})
}

// WithTenant 返回带有租户编号的上下文。
func WithTenant(ctx context.Context, tenantID int64) (context.Context, error) {
	if tenantID <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidTenantID, tenantID)
	}
	return With(ctx, tenantID, ""), nil
}

// From 取出租户上下文。
func From(ctx context.Context) (Context, bool) {
	state, ok := ctx.Value(contextKey{}).(contextState)
	return state.tenant, ok && state.tenant.ID > 0
}

// TenantID 返回当前上下文中的租户编号。
func TenantID(ctx context.Context) (int64, bool) {
	tenant, ok := From(ctx)
	return tenant.ID, ok
}

// RequiredTenantID 返回当前租户编号；未设置时返回 ErrTenantNotFound。
func RequiredTenantID(ctx context.Context) (int64, error) {
	tenant, ok := From(ctx)
	if !ok {
		return 0, ErrTenantNotFound
	}
	return tenant.ID, nil
}

// RequiredTenantCode 返回当前租户编码；未设置或编码为空时返回 ErrTenantNotFound。
func RequiredTenantCode(ctx context.Context) (string, error) {
	tenant, ok := From(ctx)
	if !ok || tenant.Code == "" {
		return "", ErrTenantNotFound
	}
	return tenant.Code, nil
}

// WithSkip 返回一个保留租户信息、但跳过行级租户过滤的上下文。
func WithSkip(ctx context.Context) context.Context {
	state, _ := ctx.Value(contextKey{}).(contextState)
	state.skip = true
	return context.WithValue(ctx, contextKey{}, state)
}

// IsSkip 判断当前上下文是否显式豁免租户过滤。
func IsSkip(ctx context.Context) bool {
	state, ok := ctx.Value(contextKey{}).(contextState)
	return ok && state.skip
}

// Ignore 是 WithSkip 的兼容别名。
func Ignore(ctx context.Context) context.Context {
	return WithSkip(ctx)
}

// IsIgnore 是 IsSkip 的兼容别名。
func IsIgnore(ctx context.Context) bool {
	return IsSkip(ctx)
}

// ExecuteIgnore 在 fn 执行期间关闭租户隔离，并返回 fn 的错误。
func ExecuteIgnore(ctx context.Context, fn func(context.Context) error) error {
	if fn == nil {
		return errors.New("tenant: ignore function is nil")
	}
	return fn(WithSkip(ctx))
}

// Scope 为 GORM 查询增加 tenant_id 条件。
//
// 未设置租户时会向 db 注入 ErrTenantNotFound，避免无意间查询跨租户数据。
// 全局任务等确需跨租户查询的代码应显式传入 Ignore(ctx)。
func Scope(db *gorm.DB, ctx context.Context, column string) *gorm.DB {
	if db == nil {
		return nil
	}
	if IsSkip(ctx) {
		return db.WithContext(ctx)
	}
	id, err := RequiredTenantID(ctx)
	if err != nil {
		scoped := db.WithContext(ctx)
		scoped.AddError(err)
		return scoped
	}
	if column == "" {
		column = "tenant_id"
	}
	return db.WithContext(ctx).Where(clause.Eq{
		Column: clause.Column{Name: column},
		Value:  id,
	})
}
