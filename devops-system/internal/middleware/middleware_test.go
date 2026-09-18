package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	securityauthz "devops-infra/security/authz"
	securityjwt "devops-infra/security/jwt"
	securitysession "devops-infra/security/session"

	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/config"
)

// testEnv 构建共享安全实现支撑的中间件测试环境。
func testEnv(t *testing.T) (*auth.Manager, func()) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	// Redis 会话存储
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sessions := securitysession.NewRedisClientStore(client, 24*time.Hour)

	// RS256 共享 JWT 管理器
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cfg := config.JWTConfig{
		SigningKey:    string(pemKey),
		KeyID:         "test-key",
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

	// 共享 Casbin authorizer（authz 门面持有）
	db, err := gorm.Open(sqlite.Open("file:middleware_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := securityauthz.New(db)
	if err != nil {
		t.Fatal(err)
	}
	authz.SetAuthorizer(authorizer)

	mgr := auth.New(cfg, tokens, sessions)
	cleanup := func() {
		authz.Close()
		_ = client.Close()
		mr.Close()
	}
	return mgr, cleanup
}

// login 用门面签发真实 token（RS256 + 会话）。
func login(t *testing.T, m *auth.Manager, uid int64, tid int64) (*auth.AccessClaims, string) {
	t.Helper()
	access, _, _, err := m.Issue(context.Background(), uid, "alice", tid, "tenant-x")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := m.Parse(access)
	if err != nil {
		t.Fatal(err)
	}
	return claims, access
}

func perform(t *testing.T, r *gin.Engine, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestJWTMiddlewareRegressions(t *testing.T) {
	m, cleanup := testEnv(t)
	defer cleanup()

	handlerCalled := false
	r := gin.New()
	r.GET("/", JWT(m), func(c *gin.Context) {
		handlerCalled = true
		claims := CurrentClaims(c)
		if claims == nil {
			t.Fatal("claims not injected")
		}
		c.Status(http.StatusNoContent)
	})

	// 缺失 Authorization 头 → 401 + 原文案
	rec := perform(t, r, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "未登录或会话已过期") {
		t.Fatalf("missing header: %d %s", rec.Code, rec.Body.String())
	}

	// 非法 token → 401 + 原文案
	rec = perform(t, r, "garbage-token")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "未登录或会话已过期") {
		t.Fatalf("bad token: %d %s", rec.Code, rec.Body.String())
	}
	if handlerCalled {
		t.Fatal("handler ran on auth failure")
	}

	// 有效 token → 204 + claims 注入
	_, access := login(t, m, 42, 7)
	rec = perform(t, r, access)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid token: %d %s", rec.Code, rec.Body.String())
	}
	if !handlerCalled {
		t.Fatal("handler did not run on valid token")
	}

	// 会话被撤销（logout/踢下线）→ 401 + 失效文案
	claims, access2 := login(t, m, 42, 7)
	m.Logout(context.Background(), claims)
	rec = perform(t, r, access2)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "登录状态已失效，请重新登录") {
		t.Fatalf("revoked session: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPermMiddlewareRegressions(t *testing.T) {
	m, cleanup := testEnv(t)
	defer cleanup()

	// 用户 42 在租户 7 无权限；用户 43 持 editor 角色（system:user:query）
	if err := authz.RebuildRolePolicies("editor", 7, []string{"system:user:query"}); err != nil {
		t.Fatal(err)
	}
	if err := authz.RebuildUserGrouping(43, 7, []string{"editor"}); err != nil {
		t.Fatal(err)
	}

	handlerCalled := false
	r := gin.New()
	r.GET("/", JWT(m), Perm("system:user:query"), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})

	// 无 claims 不可能到达（JWT 先注入），直接验证：无权限 → 403 + 原文案
	_, access42 := login(t, m, 42, 7)
	rec := perform(t, r, access42)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "无操作权限") {
		t.Fatalf("denied: %d %s", rec.Code, rec.Body.String())
	}
	if handlerCalled {
		t.Fatal("handler ran before permission middleware allowed")
	}

	// 持权限 → handler 执行（Perm 在 handler 之前生效）
	_, access43 := login(t, m, 43, 7)
	rec = perform(t, r, access43)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("allowed: %d %s", rec.Code, rec.Body.String())
	}
	if !handlerCalled {
		t.Fatal("handler did not run after permission allowed")
	}
}

func TestPlatformPermMiddlewareRegressions(t *testing.T) {
	m, cleanup := testEnv(t)
	defer cleanup()

	// 用户 44 为平台管理员；45 仅租户角色
	if err := authz.SeedPolicies(7, 44); err != nil {
		t.Fatal(err)
	}
	if err := authz.RebuildUserGrouping(45, 7, []string{"editor"}); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/", JWT(m), PlatformPerm("platform:tenant:query"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// 非平台管理员 → 403 + 原文案（g2 门禁在 Enforce 之前）
	_, access45 := login(t, m, 45, 7)
	rec := perform(t, r, access45)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "需要平台管理员权限") {
		t.Fatalf("non-admin: %d %s", rec.Code, rec.Body.String())
	}

	// 平台管理员 → 通过
	_, access44 := login(t, m, 44, 7)
	rec = perform(t, r, access44)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("platform admin: %d %s", rec.Code, rec.Body.String())
	}
}
