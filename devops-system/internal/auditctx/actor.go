// Package auditctx carries the authenticated actor through request contexts.
package auditctx

import "context"

type actorContextKey struct{}

// Actor identifies the authenticated user responsible for a database change.
type Actor struct {
	Username string
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
