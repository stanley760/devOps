package middleware

import (
	"context"
	"errors"
	"fmt"
	"strings"

	securityjwt "devops-infra/security/jwt"
	securitysession "devops-infra/security/session"
)

var (
	ErrUnauthorized    = errors.New("middleware: unauthorized")
	ErrSessionInactive = errors.New("middleware: session inactive")
	ErrForbidden       = errors.New("middleware: forbidden")
	ErrInfrastructure  = errors.New("middleware: infrastructure error")
)

type TokenParser interface {
	ParseAccess(token string) (*securityjwt.Claims, error)
}

type SessionStore interface {
	Active(ctx context.Context, ref securitysession.SessionRef) (bool, error)
}

type Authenticator struct {
	tokens   TokenParser
	sessions SessionStore
}

func NewAuthenticator(tokens TokenParser, sessions SessionStore) *Authenticator {
	return &Authenticator{tokens: tokens, sessions: sessions}
}

func (a *Authenticator) Authenticate(ctx context.Context, bearerHeader string) (*securityjwt.Claims, error) {
	if a == nil || a.tokens == nil || a.sessions == nil {
		return nil, fmt.Errorf("%w: authenticator is not initialized", ErrInfrastructure)
	}
	if !strings.HasPrefix(bearerHeader, "Bearer ") {
		return nil, ErrUnauthorized
	}

	token := strings.TrimPrefix(bearerHeader, "Bearer ")
	if token == "" {
		return nil, ErrUnauthorized
	}

	claims, err := a.tokens.ParseAccess(token)
	if err != nil {
		if errors.Is(err, securityjwt.ErrInvalidConfig) {
			return nil, fmt.Errorf("%w: %v", ErrInfrastructure, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	active, err := a.sessions.Active(ctx, securitysession.SessionRef{
		TenantID:  claims.TenantID,
		UserID:    claims.UID,
		SessionID: claims.SessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInfrastructure, err)
	}
	if !active {
		return nil, ErrSessionInactive
	}

	return claims, nil
}
