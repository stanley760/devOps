// Package session defines the shared refresh-session contract.
package session

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidTenantID = errors.New("session: tenant id must be positive")
	ErrInvalidUserID   = errors.New("session: user id must be positive")
	ErrInvalidSession  = errors.New("session: session id must not be empty")
)

type Session struct {
	TenantID  int64
	UserID    int64
	SessionID string
	TTL       time.Duration
}

type SessionRef struct {
	TenantID  int64
	UserID    int64
	SessionID string
}

type Store interface {
	Create(context.Context, Session) error
	Active(context.Context, SessionRef) (bool, error)
	Lookup(context.Context, string) (tenantID, userID int64, ok bool)
	Matches(context.Context, SessionRef, int64) (bool, error)
	Revoke(context.Context, SessionRef) error
	RevokeUser(context.Context, int64, int64) error
	RevokeTenant(context.Context, int64) error
}

func validateRef(ref SessionRef) error {
	if ref.TenantID <= 0 {
		return ErrInvalidTenantID
	}
	if ref.UserID <= 0 {
		return ErrInvalidUserID
	}
	if ref.SessionID == "" {
		return ErrInvalidSession
	}
	return nil
}

func validateSession(s Session) error {
	return validateRef(SessionRef{TenantID: s.TenantID, UserID: s.UserID, SessionID: s.SessionID})
}

func refreshKey(tenantID int64, sid string) string {
	return fmt.Sprintf("t%d:auth:refresh:%s", tenantID, sid)
}

func sidsKey(tenantID, userID int64) string {
	return fmt.Sprintf("t%d:auth:sids:%d", tenantID, userID)
}

func sid2uidKey(sid string) string { return "auth:sid2uid:" + sid }
