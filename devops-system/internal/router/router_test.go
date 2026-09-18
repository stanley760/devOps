package router

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	securityjwt "devops-infra/security/jwt"
	securitysession "devops-infra/security/session"

	"devops-system/internal/auth"
	"devops-system/internal/config"
	"devops-system/internal/service"
)

// jwksEnv 构建真实共享安全装配（RS256 + miniredis 会话）与业务路由。
func jwksEnv(t *testing.T) (*auth.Manager, *rsa.PublicKey, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cfg := config.JWTConfig{
		SigningKey:    string(pemKey),
		KeyID:         "router-test-key",
		Issuer:        "devops",
		AccessExpire:  time.Hour,
		RefreshExpire: 24 * time.Hour,
	}
	pub, err := securityjwt.PublicKeyFromPrivatePEM(pemKey)
	if err != nil {
		t.Fatal(err)
	}
	keySet, err := securityjwt.NewStaticKeySet(map[string]*rsa.PublicKey{cfg.EffectiveKeyID(): pub})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := securityjwt.NewManager(cfg.SharedJWTConfig(keySet))
	if err != nil {
		t.Fatal(err)
	}
	m := auth.New(cfg, tokens, securitysession.NewRedisClientStore(client, cfg.RefreshExpire))

	svcCfg := config.Config{}
	reg := Register(m,
		service.NewAuth(svcCfg, m), service.NewUser(m), service.NewRole(),
		service.NewMenu(), service.NewDept(), service.NewPost(), service.NewTenant(svcCfg, m))
	r := gin.New()
	reg(r)
	return m, pub, r
}

// TestJWKSEndpointVerifiesIssuedToken 模拟 Higress jwt-auth 完整验签路径：
// 拉取 /.well-known/jwks.json → 按 kid 选公钥 → RS256 PKCS#1 v1.5 验签。
func TestJWKSEndpointVerifiesIssuedToken(t *testing.T) {
	m, signingPub, r := jwksEnv(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("jwks status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("jwks body: %v", err)
	}
	if len(doc.Keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(doc.Keys))
	}
	k := doc.Keys[0]
	if k.Kty != "RSA" || k.Use != "sig" || k.Alg != "RS256" || k.Kid != "router-test-key" {
		t.Fatalf("jwks key fields: %+v", k)
	}

	// JWKS 公钥与签发私钥配对
	n, _ := base64.RawURLEncoding.DecodeString(k.N)
	e, _ := base64.RawURLEncoding.DecodeString(k.E)
	jwksPub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	if jwksPub.N.Cmp(signingPub.N) != 0 || jwksPub.E != signingPub.E {
		t.Fatal("jwks public key does not match signing key")
	}

	// 用 JWKS 公钥对签发的 access token 做 RS256 验签（等价 Higress jwt-auth 行为）
	access, _, _, err := m.Issue(context.Background(), 42, "alice", 7, "tenant-7")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		t.Fatalf("token segments = %d", len(parts))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(jwksPub, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("higress-style signature verification failed: %v", err)
	}
}
