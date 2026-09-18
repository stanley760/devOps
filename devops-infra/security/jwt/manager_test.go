package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

type testKeySet map[string]*rsa.PublicKey

func (s testKeySet) Lookup(kid string) (*rsa.PublicKey, error) {
	key, ok := s[kid]
	if !ok {
		return nil, errors.New("unknown kid")
	}
	return key, nil
}

func keyPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func validConfig(t *testing.T) Config {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return Config{PrivateKeyPEM: keyPEM(t, key), KeyID: "key-1", Issuer: "devops",
		AccessExpire: time.Hour, RefreshExpire: 24 * time.Hour,
		KeySet: testKeySet{"key-1": &key.PublicKey}}
}

func TestIssueAndParseAccess(t *testing.T) {
	cfg := validConfig(t)
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	pair, err := manager.Issue(context.Background(), Subject{UserID: 42, Username: "alice", TenantID: 7, TenantCode: "tenant-7"}, "session-123")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	header, _ := jwtlib.Parse(pair.AccessToken, func(token *jwtlib.Token) (any, error) { return cfg.KeySet.Lookup("key-1") })
	if header == nil || header.Method != jwtlib.SigningMethodRS256 || header.Header["kid"] != "key-1" {
		t.Fatalf("unexpected header")
	}
	claims, err := manager.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UID != 42 || claims.Username != "alice" || claims.SessionID != "session-123" || claims.TenantID != 7 || claims.TenantCode != "tenant-7" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
}

func TestInvalidAndRejectedTokens(t *testing.T) {
	cfg := validConfig(t)
	manager, _ := NewManager(cfg)
	claims := Claims{RegisteredClaims: jwtlib.RegisteredClaims{Issuer: cfg.Issuer, ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Hour))}}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, claims)
	token.Header["kid"] = "unknown"
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signed, _ := token.SignedString(key)
	if _, err := manager.ParseAccess(signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown kid: %v", err)
	}
}

func TestParseRejectsWrongIssuerAndNonRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{PrivateKeyPEM: keyPEM(t, key), KeyID: "key-1", Issuer: "devops",
		AccessExpire: time.Hour, RefreshExpire: time.Hour, KeySet: testKeySet{"key-1": &key.PublicKey}}
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wrongIssuer := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{Issuer: "other", ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Hour))},
	})
	wrongIssuer.Header["kid"] = cfg.KeyID
	signed, _ := wrongIssuer.SignedString(key)
	if _, err := manager.ParseAccess(signed); err == nil {
		t.Fatal("expected wrong issuer failure")
	}
	nonRS := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{Issuer: cfg.Issuer, ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(time.Hour))},
	})
	nonRS.Header["kid"] = cfg.KeyID
	hmac, _ := nonRS.SignedString([]byte("not-an-rsa-key"))
	if _, err := manager.ParseAccess(hmac); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected non-RS256 rejection, got %v", err)
	}
}

func TestConfigAndValidationFailures(t *testing.T) {
	if _, err := NewManager(Config{Issuer: "devops", AccessExpire: time.Hour, RefreshExpire: time.Hour}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config, got %v", err)
	}
	cfg := validConfig(t)
	manager, _ := NewManager(cfg)
	claims := Claims{RegisteredClaims: jwtlib.RegisteredClaims{Issuer: cfg.Issuer, ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(-time.Minute))}}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, claims)
	token.Header["kid"] = cfg.KeyID
	block, _ := pem.Decode(cfg.PrivateKeyPEM)
	key, _ := x509.ParsePKCS1PrivateKey(block.Bytes)
	signed, _ := token.SignedString(key)
	if _, err := manager.ParseAccess(signed); err == nil {
		t.Fatal("expected expired token failure")
	}
}
