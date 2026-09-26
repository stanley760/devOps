package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
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

// testEnv 构建共享安全实现支撑的中间件测试环境（huma 版）。
func testEnv(t *testing.T) (*auth.Manager, func()) {
	t.Helper()

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

// newMWAPI 组装挂载指定中间件的 humatest API（与生产 apidoc.New 对齐：禁用 $schema 注入）。
func newMWAPI(t *testing.T, m ...func(huma.Context, func(huma.Context))) humatest.TestAPI {
	t.Helper()
	cfg := huma.DefaultConfig("mw-test", "v1")
	cfg.CreateHooks = nil
	_, api := humatest.New(t, cfg)
	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/",
		Middlewares: huma.Middlewares(m),
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, nil
	})
	return api
}

// perform 带可选 Bearer 头发请求。
func perform(t *testing.T, api humatest.TestAPI, token string) int {
	t.Helper()
	if token == "" {
		return api.Get("/").Code
	}
	return api.Get("/", "Authorization: Bearer "+token).Code
}

func TestHumaJWTMiddlewareRegressions(t *testing.T) {
	m, cleanup := testEnv(t)
	defer cleanup()

	api := newMWAPI(t, HumaJWT(m))

	// 缺失 Authorization 头 → 401 + 原文案信封
	resp := api.Get("/")
	if resp.Code != http.StatusUnauthorized || !strings.Contains(resp.Body.String(), "未登录或会话已过期") {
		t.Fatalf("missing header: %d %s", resp.Code, resp.Body.String())
	}

	// 非法 token → 401 + 原文案
	resp = api.Get("/", "Authorization: Bearer garbage-token")
	if resp.Code != http.StatusUnauthorized || !strings.Contains(resp.Body.String(), "未登录或会话已过期") {
		t.Fatalf("bad token: %d %s", resp.Code, resp.Body.String())
	}

	// 有效 token → 204（中间件放行，handler 执行）
	_, access := login(t, m, 42, 7)
	if code := perform(t, api, access); code != http.StatusNoContent {
		t.Fatalf("valid token: %d", code)
	}

	// 会话被撤销（logout/踢下线）→ 401 + 失效文案
	claims, access2 := login(t, m, 42, 7)
	m.Logout(context.Background(), claims)
	resp = api.Get("/", "Authorization: Bearer "+access2)
	if resp.Code != http.StatusUnauthorized || !strings.Contains(resp.Body.String(), "登录状态已失效，请重新登录") {
		t.Fatalf("revoked session: %d %s", resp.Code, resp.Body.String())
	}
}

func TestHumaPermMiddlewareRegressions(t *testing.T) {
	m, cleanup := testEnv(t)
	defer cleanup()

	// 用户 42 在租户 7 无权限；用户 43 持 editor 角色（system:user:query）
	if err := authz.RebuildRolePolicies("editor", 7, []string{"system:user:query"}); err != nil {
		t.Fatal(err)
	}
	if err := authz.RebuildUserGrouping(43, 7, []string{"editor"}); err != nil {
		t.Fatal(err)
	}

	api := newMWAPI(t, HumaJWT(m), HumaPerm("system:user:query"))

	// 无权限 → 403 + 原文案
	_, access42 := login(t, m, 42, 7)
	resp := api.Get("/", "Authorization: Bearer "+access42)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "无操作权限") {
		t.Fatalf("denied: %d %s", resp.Code, resp.Body.String())
	}

	// 持权限 → 204
	_, access43 := login(t, m, 43, 7)
	resp = api.Get("/", "Authorization: Bearer "+access43)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("allowed: %d %s", resp.Code, resp.Body.String())
	}
}

func TestHumaPlatformPermMiddlewareRegressions(t *testing.T) {
	m, cleanup := testEnv(t)
	defer cleanup()

	// 用户 44 为平台管理员；45 仅租户角色
	if err := authz.SeedPolicies(7, 44); err != nil {
		t.Fatal(err)
	}
	if err := authz.RebuildUserGrouping(45, 7, []string{"editor"}); err != nil {
		t.Fatal(err)
	}

	api := newMWAPI(t, HumaJWT(m), HumaPlatformPerm("platform:tenant:query"))

	// 非平台管理员 → 403 + 原文案（g2 门禁在 Enforce 之前）
	_, access45 := login(t, m, 45, 7)
	resp := api.Get("/", "Authorization: Bearer "+access45)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "需要平台管理员权限") {
		t.Fatalf("non-admin: %d %s", resp.Code, resp.Body.String())
	}

	// 平台管理员 → 204
	_, access44 := login(t, m, 44, 7)
	resp = api.Get("/", "Authorization: Bearer "+access44)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("platform admin: %d %s", resp.Code, resp.Body.String())
	}
}
