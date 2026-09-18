package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

func validConfig() Config {
	return Config{
		SigningKey:    "test-secret",
		Issuer:        "devops",
		AccessExpire:  time.Hour,
		RefreshExpire: 24 * time.Hour,
	}
}

func mustSignToken(t *testing.T, method jwtlib.SigningMethod, claims Claims, key string) string {
	t.Helper()
	token := jwtlib.NewWithClaims(method, claims)
	signed, err := token.SignedString([]byte(key))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func hasField(payload []byte, key string) bool {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(payload, &body); err != nil {
		return false
	}
	_, ok := body[key]
	return ok
}

func TestIssueAndParseAccess(t *testing.T) {
	manager, err := NewManager(validConfig())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	subject := Subject{
		UserID:     42,
		Username:   "alice",
		TenantID:   7,
		TenantCode: "tenant-7",
	}

	pair, err := manager.Issue(context.Background(), subject, "session-123")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if pair.SessionID != "session-123" {
		t.Fatalf("expected session id session-123, got %q", pair.SessionID)
	}
	if pair.AccessExpiresIn != int64(validConfig().AccessExpire.Seconds()) {
		t.Fatalf("expected access expiry %d, got %d", int64(validConfig().AccessExpire.Seconds()), pair.AccessExpiresIn)
	}

	parsed, err := manager.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.UID != subject.UserID {
		t.Fatalf("expected uid %d, got %d", subject.UserID, parsed.UID)
	}
	if parsed.Username != subject.Username {
		t.Fatalf("expected username %q, got %q", subject.Username, parsed.Username)
	}
	if parsed.SessionID != "session-123" {
		t.Fatalf("expected sid session-123, got %q", parsed.SessionID)
	}
	if parsed.TenantID != subject.TenantID {
		t.Fatalf("expected tid %d, got %d", subject.TenantID, parsed.TenantID)
	}
	if parsed.TenantCode != subject.TenantCode {
		t.Fatalf("expected tcode %q, got %q", subject.TenantCode, parsed.TenantCode)
	}

	payload, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	for _, field := range []string{"uid", "username", "sid", "tid", "tcode"} {
		if !hasField(payload, field) {
			t.Fatalf("missing claim %q in payload %s", field, payload)
		}
	}
}

func TestIssueRejectsInvalidConfig(t *testing.T) {
	if _, err := NewManager(Config{Issuer: "devops", AccessExpire: time.Hour, RefreshExpire: 24 * time.Hour}); err == nil {
		t.Fatal("expected invalid config when signing key is missing")
	}
}

func TestParseAccessExpired(t *testing.T) {
	cfg := validConfig()
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	claims := Claims{
		UID:        42,
		Username:   "alice",
		SessionID:  "session-123",
		TenantID:   7,
		TenantCode: "tenant-7",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   "42",
			IssuedAt:  jwtlib.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}

	token := mustSignToken(t, jwtlib.SigningMethodHS256, claims, cfg.SigningKey)
	if _, err := manager.ParseAccess(token); err == nil {
		t.Fatal("expected expired token to fail")
	}
}

func TestParseAccessWrongIssuer(t *testing.T) {
	cfg := validConfig()
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	claims := Claims{
		UID:        42,
		Username:   "alice",
		SessionID:  "session-123",
		TenantID:   7,
		TenantCode: "tenant-7",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    "wrong-issuer",
			Subject:   "42",
			IssuedAt:  jwtlib.NewNumericDate(time.Now()),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	token := mustSignToken(t, jwtlib.SigningMethodHS256, claims, cfg.SigningKey)
	if _, err := manager.ParseAccess(token); err == nil {
		t.Fatal("expected wrong issuer to fail")
	}
}

func TestParseAccessRejectsNonHS256(t *testing.T) {
	cfg := validConfig()
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	claims := Claims{
		UID:        42,
		Username:   "alice",
		SessionID:  "session-123",
		TenantID:   7,
		TenantCode: "tenant-7",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   "42",
			IssuedAt:  jwtlib.NewNumericDate(time.Now()),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	token := mustSignToken(t, jwtlib.SigningMethodHS512, claims, cfg.SigningKey)
	if _, err := manager.ParseAccess(token); err == nil || !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected non-HS256 token to fail with ErrInvalidToken, got %v", err)
	}
}

func TestParseAccessRejectsNonHMAC(t *testing.T) {
	cfg := validConfig()
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	claims := Claims{
		UID:        42,
		Username:   "alice",
		SessionID:  "session-123",
		TenantID:   7,
		TenantCode: "tenant-7",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   "42",
			IssuedAt:  jwtlib.NewNumericDate(time.Now()),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, claims)
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign rsa token: %v", err)
	}

	if _, err := manager.ParseAccess(signed); err == nil || !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected non-HMAC token to fail with ErrInvalidToken, got %v", err)
	}
}
