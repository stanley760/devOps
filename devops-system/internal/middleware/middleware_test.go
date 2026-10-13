package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"github.com/alicebob/miniredis/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/glebarez/sqlite"
	goredis "github.com/redis/go-redis/v9"
	memcache "github.com/stanley760/devOps/devops-infra/cache/memory"
	securityauthz "github.com/stanley760/devOps/devops-infra/security/authz"
	securityjwt "github.com/stanley760/devOps/devops-infra/security/jwt"
	securitysession "github.com/stanley760/devOps/devops-infra/security/session"
	"github.com/stanley760/devOps/devops-system/internal/auditctx"
	"github.com/stanley760/devOps/devops-system/internal/auth"
	"github.com/stanley760/devOps/devops-system/internal/authz"
	"github.com/stanley760/devOps/devops-system/internal/config"
	"github.com/stanley760/devOps/devops-system/internal/model"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"testing"
	"time"
)

// testEnv 构建共享安全实现支撑的中间件测试环境（huma 版）。
// 返回 auth 门面、casbin authorizer、底层 sqlite 连接（租户表种子用）。
func testEnv(t *testing.T) (*auth.Manager, securityauthz.Authorizer, *gorm.DB, func()) {
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
	return mgr, authorizer, db, cleanup
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
	m, authorizer, _, cleanup := testEnv(t)
	defer cleanup()

	kit := NewKit(m, Deps{Authorizer: authorizer})
	api := newMWAPI(t, kit.JWT())

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

func TestHumaJWTMiddlewareInjectsAuditActor(t *testing.T) {
	m, authorizer, _, cleanup := testEnv(t)
	defer cleanup()

	kit := NewKit(m, Deps{Authorizer: authorizer})
	cfg := huma.DefaultConfig("audit-actor-test", "v1")
	cfg.CreateHooks = nil
	_, api := humatest.New(t, cfg)

	var actor auditctx.Actor
	var actorOK bool
	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/",
		Middlewares: huma.Middlewares{kit.JWT()},
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		actor, actorOK = auditctx.From(ctx)
		return nil, nil
	})

	_, access := login(t, m, 42, 7)
	if code := api.Get("/", "Authorization: Bearer "+access).Code; code != http.StatusNoContent {
		t.Fatalf("authenticated request: %d", code)
	}
	if !actorOK || actor.Username != "alice" {
		t.Fatalf("audit actor = (%q, %v), want (alice, true)", actor.Username, actorOK)
	}
}

func TestHumaPermMiddlewareRegressions(t *testing.T) {
	m, authorizer, _, cleanup := testEnv(t)
	defer cleanup()

	// 用户 42 在租户 7 无权限；用户 43 持 editor 角色（system:user:query）
	if err := authz.RebuildRolePolicies("editor", 7, []string{"system:user:query"}); err != nil {
		t.Fatal(err)
	}
	if err := authz.RebuildUserGrouping(43, 7, []string{"editor"}); err != nil {
		t.Fatal(err)
	}

	kit := NewKit(m, Deps{Authorizer: authorizer})
	api := newMWAPI(t, kit.JWT(), kit.Perm("system:user:query"))

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
		t.Fatalf("allowed: %d", resp.Code)
	}
}

func TestHumaPlatformPermMiddlewareRegressions(t *testing.T) {
	m, authorizer, _, cleanup := testEnv(t)
	defer cleanup()

	// 用户 44 为平台管理员；45 仅租户角色
	if err := authz.SeedPolicies(7, 44); err != nil {
		t.Fatal(err)
	}
	if err := authz.RebuildUserGrouping(45, 7, []string{"editor"}); err != nil {
		t.Fatal(err)
	}

	kit := NewKit(m, Deps{Authorizer: authorizer})
	api := newMWAPI(t, kit.JWT(), kit.Plat("platform:tenant:query"))

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
		t.Fatalf("platform admin: %d", resp.Code)
	}
}

// seedTenants 在 sqlite 中建租户表并种入三态租户：7 正常、8 停用、9 过期。
func seedTenants(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&model.Tenant{}); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-time.Hour)
	tenants := []model.Tenant{
		{BaseModel: model.BaseModel{ID: 7}, Name: "正常租户", Code: "t7", Status: 1},
		{BaseModel: model.BaseModel{ID: 8}, Name: "停用租户", Code: "t8", Status: 0},
		{BaseModel: model.BaseModel{ID: 9}, Name: "过期租户", Code: "t9", Status: 1, ExpireTime: &expired},
	}
	if err := db.Create(&tenants).Error; err != nil {
		t.Fatal(err)
	}
}

// TestHumaTenantMiddlewareStates 租户可用性三态回归：正常放行、停用/过期 403 信封；
// 顺带验证 Redis 缓存命中路径（正常租户二次请求走缓存）。
func TestHumaTenantMiddlewareStates(t *testing.T) {
	m, authorizer, db, cleanup := testEnv(t)
	defer cleanup()
	seedTenants(t, db)

	c := memcache.New()
	kit := NewKit(m, Deps{TenantUsable: newTenantChecker(db, c), Authorizer: authorizer})
	api := newMWAPI(t, kit.JWT(), kit.Tenant())

	// 正常租户 → 204（首次回源查库并写缓存）
	_, access7 := login(t, m, 42, 7)
	if code := perform(t, api, access7); code != http.StatusNoContent {
		t.Fatalf("usable tenant: %d", code)
	}
	// 缓存命中路径：同租户二次请求
	if code := perform(t, api, access7); code != http.StatusNoContent {
		t.Fatalf("usable tenant (cache hit): %d", code)
	}

	// 停用租户 → 403 + 原文案
	_, access8 := login(t, m, 42, 8)
	resp := api.Get("/", "Authorization: Bearer "+access8)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "租户不可用（停用或已过期）") {
		t.Fatalf("disabled tenant: %d %s", resp.Code, resp.Body.String())
	}

	// 过期租户 → 403 + 原文案
	_, access9 := login(t, m, 42, 9)
	resp = api.Get("/", "Authorization: Bearer "+access9)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), "租户不可用（停用或已过期）") {
		t.Fatalf("expired tenant: %d %s", resp.Code, resp.Body.String())
	}

	// 无租户记录（不存在）→ 403
	_, access99 := login(t, m, 42, 99)
	resp = api.Get("/", "Authorization: Bearer "+access99)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("missing tenant: %d %s", resp.Code, resp.Body.String())
	}
}
