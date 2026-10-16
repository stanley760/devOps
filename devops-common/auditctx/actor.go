// Package auditctx carries the authenticated actor through request contexts.
//
// 跨业务契约：任何需要记录"谁触发了这次变更"的模块都从ctx 取当前操作者，
// 落库时机由各业务的模型 hook 或审计中间件决定。
package auditctx

import "context"

type actorContextKey struct{}

// Actor identifies the authenticated user responsible for a database change.
type Actor struct {
	Username string
	// UserID 操作者主键；未知或系统写入时为 0。
	UserID int64
	// UserType 操作者类别（面向 C 端会员 / 面向 B 端管理员）。
	// 语义由 devops-common/audit.UserType 定义；此处用int64 以避免包间循环依赖。
	UserType int64
	// TenantID 操作者所属租户；全局表操作时为 0。
	TenantID int64
}

// WithActor returns a context carrying the authenticated actor.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// From returns the actor attached to ctx, if any.
func From(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorContextKey{}).(Actor)
	return actor, ok
}

// Username 返回 ctx 中的操作者用户名；未注入时返回空串。
func Username(ctx context.Context) string {
	actor, _ := From(ctx)
	return actor.Username
}
