package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	securityauthz "devops-infra/security/authz"
	securityjwt "devops-infra/security/jwt"
	securitysession "devops-infra/security/session"
)

type stubTokenParser struct {
	claims *securityjwt.Claims
	err    error
	token  string
}

func (s *stubTokenParser) ParseAccess(token string) (*securityjwt.Claims, error) {
	s.token = token
	if s.err != nil {
		return nil, s.err
	}
	return s.claims, nil
}

type stubSessionStore struct {
	active bool
	err    error
	ref    securitysession.SessionRef
}

func (s *stubSessionStore) Active(ctx context.Context, ref securitysession.SessionRef) (bool, error) {
	s.ref = ref
	return s.active, s.err
}

type stubAuthorizer struct {
	enforceOK          bool
	enforceErr         error
	enforcePlatformOK  bool
	enforcePlatformErr error
	platformAdmin      bool
	platformAdminErr   error
}

func (s *stubAuthorizer) Enforce(ctx context.Context, subject, domain, permission string) (bool, error) {
	return s.enforceOK, s.enforceErr
}

func (s *stubAuthorizer) EnforcePlatform(ctx context.Context, subject, permission string) (bool, error) {
	return s.enforcePlatformOK, s.enforcePlatformErr
}

func (s *stubAuthorizer) IsPlatformAdmin(ctx context.Context, subject string) (bool, error) {
	return s.platformAdmin, s.platformAdminErr
}

func testOptions() GinOptions {
	return GinOptions{
		OnUnauthorized: func(c *gin.Context, err error) {
			c.String(http.StatusUnauthorized, err.Error())
		},
		OnForbidden: func(c *gin.Context, err error) {
			c.String(http.StatusForbidden, err.Error())
		},
		OnInfrastructureError: func(c *gin.Context, err error) {
			c.String(http.StatusServiceUnavailable, err.Error())
		},
	}
}

func validClaims() *securityjwt.Claims {
	return &securityjwt.Claims{
		UID:        42,
		Username:   "alice",
		SessionID:  "sid-1",
		TenantID:   7,
		TenantCode: "tenant-7",
	}
}

func TestAuthenticatorAuthenticate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		header       string
		parserErr    error
		sessionOK    bool
		sessionErr   error
		wantErr      error
		wantToken    string
		wantSession  securitysession.SessionRef
	}{
		{
			name:    "missing header",
			wantErr: ErrUnauthorized,
		},
		{
			name:    "wrong scheme",
			header:  "Basic token",
			wantErr: ErrUnauthorized,
		},
		{
			name:    "empty bearer token",
			header:  "Bearer ",
			wantErr: ErrUnauthorized,
		},
		{
			name:      "invalid token",
			header:    "Bearer bad-token",
			parserErr: securityjwt.ErrInvalidToken,
			wantErr:   ErrUnauthorized,
			wantToken: "bad-token",
		},
		{
			name:       "inactive session",
			header:     "Bearer active-token",
			sessionOK:  false,
			wantErr:    ErrUnauthorized,
			wantToken:  "active-token",
			wantSession: securitysession.SessionRef{
				TenantID:  7,
				UserID:    42,
				SessionID: "sid-1",
			},
		},
		{
			name:       "session infrastructure error",
			header:     "Bearer active-token",
			sessionErr: errors.New("redis down"),
			wantErr:    ErrInfrastructure,
			wantToken:  "active-token",
			wantSession: securitysession.SessionRef{
				TenantID:  7,
				UserID:    42,
				SessionID: "sid-1",
			},
		},
		{
			name:      "valid token",
			header:    "Bearer active-token",
			sessionOK: true,
			wantToken: "active-token",
			wantSession: securitysession.SessionRef{
				TenantID:  7,
				UserID:    42,
				SessionID: "sid-1",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parser := &stubTokenParser{claims: validClaims(), err: tt.parserErr}
			sessions := &stubSessionStore{active: tt.sessionOK, err: tt.sessionErr}
			authenticator := NewAuthenticator(parser, sessions)

			claims, err := authenticator.Authenticate(context.Background(), tt.header)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && claims == nil {
				t.Fatal("Authenticate() claims = nil")
			}
			if parser.token != tt.wantToken {
				t.Fatalf("token = %q, want %q", parser.token, tt.wantToken)
			}
			if sessions.ref != tt.wantSession {
				t.Fatalf("session ref = %+v, want %+v", sessions.ref, tt.wantSession)
			}
		})
	}
}

func TestGinAuthenticateMiddleware(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	parser := &stubTokenParser{claims: validClaims()}
	sessions := &stubSessionStore{active: true}
	authenticator := NewAuthenticator(parser, sessions)

	router := gin.New()
	router.GET("/", GinAuthenticate(authenticator, testOptions()), func(c *gin.Context) {
		claims := CurrentClaims(c)
		if claims == nil || claims.UID != 42 {
			t.Fatalf("claims not injected: %+v", claims)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer active-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestGinPermissionMiddleware(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	t.Run("permission denied aborts request", func(t *testing.T) {
		parser := &stubTokenParser{claims: validClaims()}
		sessions := &stubSessionStore{active: true}
		authorizer := &stubAuthorizer{enforceOK: false}

		router := gin.New()
		router.GET("/", GinAuthenticate(NewAuthenticator(parser, sessions), testOptions()),
			GinRequirePermission(authorizer, "system:user:query", testOptions()),
			func(c *gin.Context) {
				t.Fatal("handler should not run")
			})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer active-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("authorizer error maps to infrastructure failure", func(t *testing.T) {
		parser := &stubTokenParser{claims: validClaims()}
		sessions := &stubSessionStore{active: true}
		authorizer := &stubAuthorizer{enforceErr: errors.New("casbin unavailable")}

		router := gin.New()
		router.GET("/", GinAuthenticate(NewAuthenticator(parser, sessions), testOptions()),
			GinRequirePermission(authorizer, "system:user:query", testOptions()),
			func(c *gin.Context) {
				t.Fatal("handler should not run")
			})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer active-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("permission success calls next", func(t *testing.T) {
		parser := &stubTokenParser{claims: validClaims()}
		sessions := &stubSessionStore{active: true}
		authorizer := &stubAuthorizer{enforceOK: true}

		router := gin.New()
		router.GET("/", GinAuthenticate(NewAuthenticator(parser, sessions), testOptions()),
			GinRequirePermission(authorizer, "system:user:query", testOptions()),
			func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer active-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
		}
	})
}

func TestGinPlatformPermissionMiddleware(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	t.Run("platform admin denial returns forbidden", func(t *testing.T) {
		parser := &stubTokenParser{claims: validClaims()}
		sessions := &stubSessionStore{active: true}
		authorizer := &stubAuthorizer{platformAdmin: false}

		router := gin.New()
		router.GET("/", GinAuthenticate(NewAuthenticator(parser, sessions), testOptions()),
			GinRequirePlatformPermission(authorizer, "platform:tenant:query", testOptions()),
			func(c *gin.Context) {
				t.Fatal("handler should not run")
			})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer active-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("platform permission success calls next", func(t *testing.T) {
		parser := &stubTokenParser{claims: validClaims()}
		sessions := &stubSessionStore{active: true}
		authorizer := &stubAuthorizer{platformAdmin: true, enforcePlatformOK: true}

		router := gin.New()
		router.GET("/", GinAuthenticate(NewAuthenticator(parser, sessions), testOptions()),
			GinRequirePlatformPermission(authorizer, "platform:tenant:query", testOptions()),
			func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer active-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
		}
	})
}

var _ securityauthz.Authorizer = (*stubAuthorizer)(nil)
