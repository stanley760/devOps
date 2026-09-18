package jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
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
	if _, err := NewManager(Config{PrivateKeyPEM: []byte("not-a-pem"), KeyID: "key-1", Issuer: "devops",
		AccessExpire: time.Hour, RefreshExpire: time.Hour, KeySet: testKeySet{}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected malformed pem config error, got %v", err)
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

func TestParseRequiresExpiration(t *testing.T) {
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
	// 无 exp 的签名有效 token 也必须拒绝（验证侧不信任签发方总写 exp）。
	noExp := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, Claims{
		RegisteredClaims: jwtlib.RegisteredClaims{Issuer: cfg.Issuer, IssuedAt: jwtlib.NewNumericDate(time.Now())},
	})
	noExp.Header["kid"] = cfg.KeyID
	signed, _ := noExp.SignedString(key)
	if _, err := manager.ParseAccess(signed); err == nil {
		t.Fatal("expected missing-exp rejection")
	}
}

func TestStaticKeySetAndPublicKeyFromPrivatePEM(t *testing.T) {
	if _, err := NewStaticKeySet(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected empty keyset error, got %v", err)
	}
	if _, err := NewStaticKeySet(map[string]*rsa.PublicKey{"": nil}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid entry error, got %v", err)
	}
	cfg := validConfig(t)
	manager, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := PublicKeyFromPrivatePEM(cfg.PrivateKeyPEM)
	if err != nil {
		t.Fatalf("derive public key: %v", err)
	}
	set, err := NewStaticKeySet(map[string]*rsa.PublicKey{"key-1": pub})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Lookup("missing"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected unknown kid error, got %v", err)
	}
	got, err := set.Lookup("key-1")
	if err != nil || got.N.Cmp(pub.N) != 0 || got.E != pub.E {
		t.Fatalf("lookup mismatch: %v", err)
	}
	_ = manager
}

func TestJWKSDocumentRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := JWKSDocument(&key.PublicKey, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("jwks json: %v", err)
	}
	if len(parsed.Keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(parsed.Keys))
	}
	k := parsed.Keys[0]
	if k.Kty != "RSA" || k.Kid != "key-1" || k.Use != "sig" || k.Alg != "RS256" {
		t.Fatalf("jwks fields mismatch: %+v", k)
	}
	n, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		t.Fatalf("decode n: %v", err)
	}
	e, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		t.Fatalf("decode e: %v", err)
	}
	got := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	if got.N.Cmp(key.PublicKey.N) != 0 || got.E != key.PublicKey.E {
		t.Fatal("jwks round-trip key mismatch")
	}
	if _, err := JWKSDocument(nil, "key-1"); err == nil {
		t.Fatal("expected nil public key error")
	}
	if _, err := JWKSDocument(&key.PublicKey, ""); err == nil {
		t.Fatal("expected missing kid error")
	}
}
