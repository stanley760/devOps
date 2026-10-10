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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"

	httpinfra "devops-infra/http"
	securityjwt "devops-infra/security/jwt"
	securitysession "devops-infra/security/session"

	"devops-system/internal/auth"
	"devops-system/internal/config"
	"devops-system/internal/middleware"
	"devops-system/internal/service"
)

// newTestKit 构造测试中间件套件（无 DB/缓存环境；测试路径只到 JWT 层）。
func newTestKit(m *auth.Manager) *middleware.Kit {
	return middleware.NewKit(m, middleware.Deps{})
}

func newTestRegister(docCfg config.APIDocConfig, kit *middleware.Kit, authM *auth.Manager, svcCfg config.Config) httpinfra.Register {
	modules := Modules{
		Auth:      NewAuthRoutes(service.NewAuth(svcCfg, authM), kit),
		Users:     NewUserRoutes(service.NewUser(authM, func() {}), kit),
		Roles:     NewRoleRoutes(service.NewRole(func() {}), kit),
		Menus:     NewMenuRoutes(service.NewMenu(), kit),
		Depts:     NewDeptRoutes(service.NewDept(), kit),
		Posts:     NewPostRoutes(service.NewPost(), kit),
		Tenants:   NewTenantRoutes(service.NewTenant(authM), kit),
		LoginLogs: NewLoginLogRoutes(service.NewLoginLog(), kit),
	}
	return NewRegister(docCfg, authM, modules)
}

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
	reg := newTestRegister(config.APIDocConfig{Enabled: true, BasePath: "/apidoc"}, newTestKit(m), m, svcCfg)
	r := gin.New()
	reg(r)
	return m, pub, r
}

func TestRegisterAPIDoc(t *testing.T) {
	_, _, r := jwksEnv(t)

	docResponse := httptest.NewRecorder()
	r.ServeHTTP(docResponse, httptest.NewRequest(http.MethodGet, "/apidoc/openapi.json", nil))
	if docResponse.Code != http.StatusOK {
		t.Fatalf("openapi.json status = %d, body = %s", docResponse.Code, docResponse.Body.String())
	}

	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string             `json:"operationId"`
			Tags        []string           `json:"tags"`
			Security    []map[string][]any `json:"security"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type string `json:"type"`
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(docResponse.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	if _, ok := doc.Paths["/api/v1/auth/login"]; !ok {
		t.Fatalf("login operation missing: %+v", doc.Paths)
	}
	if _, ok := doc.Paths["/api/v1/auth/refresh"]; !ok {
		t.Fatalf("refresh operation missing: %+v", doc.Paths)
	}
	// 业务接口计数：apidoc 文档启用时自注册 export-openapi（tag "apidoc"），须排除
	operationCount := 0
	for _, path := range doc.Paths {
		for _, op := range path {
			if slices.Contains(op.Tags, "apidoc") {
				continue
			}
			operationCount++
		}
	}
	if operationCount != 43 {
		t.Fatalf("business operation count = %d, want 43", operationCount)
	}
	bearer, ok := doc.Components.SecuritySchemes["bearerAuth"]
	if !ok || bearer.Type != "http" {
		t.Fatalf("bearerAuth scheme missing or invalid: %+v", doc.Components.SecuritySchemes)
	}
	tenant, ok := doc.Components.SecuritySchemes["tenantId"]
	if !ok || tenant.Type != "apiKey" || tenant.Name != "X-Tenant-ID" || tenant.In != "header" {
		t.Fatalf("tenantId scheme missing or invalid: %+v", doc.Components.SecuritySchemes)
	}
}

// TestRegisterAPIDocEnabledKeepsAuthRoutes 回归：apidoc 开启时 login/refresh 改由
// huma 注册，其余 auth 路由（gin 注册）必须仍然可用——早期版本 authH 在开启时为 nil，
// logout/info 等路由调用即 panic（nil handler 解引用）。
func TestRegisterAPIDocEnabledKeepsAuthRoutes(t *testing.T) {
	_, _, r := jwksEnv(t)

	// 无 token：JWT 中间件应返回 401 信封，而不是 nil handler panic
	logout := httptest.NewRecorder()
	r.ServeHTTP(logout, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	if logout.Code != http.StatusUnauthorized {
		t.Fatalf("logout without token status = %d, want 401, body = %s", logout.Code, logout.Body.String())
	}

	info := httptest.NewRecorder()
	r.ServeHTTP(info, httptest.NewRequest(http.MethodGet, "/api/v1/auth/info", nil))
	if info.Code != http.StatusUnauthorized {
		t.Fatalf("info without token status = %d, want 401, body = %s", info.Code, info.Body.String())
	}

	// huma 注册的 login 路由可达：请求应命中（参数校验/内容协商错误均可），而非 404
	login := httptest.NewRecorder()
	r.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{}")))
	if login.Code == http.StatusNotFound {
		t.Fatalf("huma login route missing: %s", login.Body.String())
	}
}

func TestRegisterAPIDocDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	jwtCfg := config.JWTConfig{
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
	keySet, err := securityjwt.NewStaticKeySet(map[string]*rsa.PublicKey{jwtCfg.EffectiveKeyID(): pub})
	if err != nil {
		t.Fatal(err)
	}
	tokenMgr, err := securityjwt.NewManager(jwtCfg.SharedJWTConfig(keySet))
	if err != nil {
		t.Fatal(err)
	}
	authMgr := auth.New(jwtCfg, tokenMgr, securitysession.NewRedisClientStore(client, jwtCfg.RefreshExpire))
	svcCfg := config.Config{}

	r := gin.New()
	newTestRegister(config.APIDocConfig{}, newTestKit(authMgr), authMgr, svcCfg)(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/apidoc/openapi.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled apidoc status = %d, want 404", rec.Code)
	}

	// D5：Enabled 只门控文档端点，业务路由恒注册（auth 已迁 huma，禁用文档时仍可达）
	login := httptest.NewRecorder()
	r.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))
	if login.Code == http.StatusNotFound {
		t.Fatalf("auth route missing when apidoc disabled: %s", login.Body.String())
	}

	logout := httptest.NewRecorder()
	r.ServeHTTP(logout, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	if logout.Code != http.StatusUnauthorized {
		t.Fatalf("logout without token status = %d, want 401, body = %s", logout.Code, logout.Body.String())
	}
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
