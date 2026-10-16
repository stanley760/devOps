package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/stanley760/devops/devops-infra/security/gateway"
	securityjwt "github.com/stanley760/devops/devops-infra/security/jwt"
	securitymw "github.com/stanley760/devops/devops-infra/security/middleware"
)

// stubAuthenticator 按固定规则判定 Bearer 凭据，隔离 JWT 签发与 Redis 会话细节。
// validToken 通过校验；其余一律返回 ErrUnauthorized（与真实实现同语义）。
type stubAuthenticator struct {
	validToken string
	err        error
	calls      int
}

func (s *stubAuthenticator) Authenticate(_ context.Context, bearer string) (*securityjwt.Claims, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if bearer == "Bearer "+s.validToken {
		return &securityjwt.Claims{}, nil
	}
	return nil, errors.New("invalid token")
}

const validToken = "valid-jwt-token"

// newEngine 装配一条最小链路：守卫 -> 业务 handler。
func newEngine(t *testing.T, authn securitymw.BearerAuthenticator, requireJWT bool, exemptPaths, exemptPrefixes []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gateway.Enforce(requireJWT, authn, exemptPaths, exemptPrefixes))
	r.GET("/api/v1/auth/info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	return r
}

func do(t *testing.T, r *gin.Engine, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 携带有效 JWT 的请求必须放行。
func TestEnforce_ValidJWTPasses(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{validToken: validToken}, true, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", "Bearer "+validToken); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

// 无凭据必须 401。
func TestEnforce_MissingCredentialRejected(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{validToken: validToken}, true, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// 无效/过期 JWT 必须 401。
func TestEnforce_InvalidJWTRejected(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{validToken: validToken}, true, nil, nil)
	for _, bearer := range []string{"Bearer expired-token", "Bearer ", "garbage"} {
		if w := do(t, r, "/api/v1/auth/info", bearer); w.Code != http.StatusUnauthorized {
			t.Fatalf("bearer=%q status = %d, want %d", bearer, w.Code, http.StatusUnauthorized)
		}
	}
}

// 守卫关闭时直通，即使认证器会拒绝一切（保证本地 go run 零改动）。
func TestEnforce_DisabledPassthrough(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{err: errors.New("always fail")}, false, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// fail-closed：声明启用却没装配校验器时必须拒绝，而不是静默放行。
func TestEnforce_NilAuthenticatorFailsClosed(t *testing.T) {
	r := newEngine(t, nil, true, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", "Bearer anything"); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// 关闭守卫时即便 authn 为 nil 也不影响（不应触发 fail-closed 分支）。
func TestEnforce_DisabledWithNilAuthenticator(t *testing.T) {
	r := newEngine(t, nil, false, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// 放行名单内的精确路径不经校验（健康检查等不经网关）。
func TestEnforce_ExemptPaths(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{err: errors.New("always fail")}, true, []string{"/healthz"}, nil)
	if w := do(t, r, "/healthz", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// 放行名单内的前缀不经校验（如 /apidoc）。
func TestEnforce_ExemptPrefixes(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{err: errors.New("always fail")}, true, nil, []string{"/apidoc"})
	r.GET("/apidoc/openapi.json", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	if w := do(t, r, "/apidoc/openapi.json", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// 放行名单外的前缀相似路径不得被误放行（/apidoc-extra 不应命中 /apidoc 前缀豁免之外…
// 但按语义前缀豁免本就匹配，故此处验证的是精确路径豁免不越界）。
func TestEnforce_ExemptExactDoesNotLeak(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{err: errors.New("always fail")}, true, []string{"/healthz"}, nil)
	if w := do(t, r, "/api/v1/auth/info", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// 失败响应沿用统一信封（code 与 HTTP 状态一致）。
func TestEnforce_ErrorEnvelope(t *testing.T) {
	r := newEngine(t, &stubAuthenticator{validToken: validToken}, true, nil, nil)
	w := do(t, r, "/api/v1/auth/info", "")
	var body struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if body.Code != http.StatusUnauthorized {
		t.Fatalf("body.code = %d, want %d", body.Code, http.StatusUnauthorized)
	}
	if body.Msg == "" {
		t.Fatal("body.msg 不应为空")
	}
}

// 放行路径不得触发认证器（避免无谓的签名校验开销）。
func TestEnforce_ExemptSkipsAuthenticator(t *testing.T) {
	authn := &stubAuthenticator{err: errors.New("always fail")}
	r := newEngine(t, authn, true, []string{"/healthz"}, nil)
	do(t, r, "/healthz", "")
	do(t, r, "/api/v1/auth/info", "")
	if authn.calls != 1 {
		t.Fatalf("authenticator 调用次数 = %d, want 1（仅非放行路径应调用）", authn.calls)
	}
}

// EnforceFromConfig：RequireJWT=false 时直通。
func TestEnforceFromConfig_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gateway.EnforceFromConfig(gateway.Config{RequireJWT: false}, &stubAuthenticator{err: errors.New("fail")}, nil, nil))
	r.GET("/api/v1/auth/info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/info", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// EnforceFromConfig：启用后生效，且配置追加的 exempt_paths 生效。
func TestEnforceFromConfig_EnabledWithExempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authn := &stubAuthenticator{validToken: validToken}
	r.Use(gateway.EnforceFromConfig(
		gateway.Config{RequireJWT: true, ExemptPaths: []string{"/healthz"}},
		authn, []string{"/readyz"}, nil,
	))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/api/v1/auth/info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	for _, p := range []string{"/healthz", "/readyz"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", p, w.Code, http.StatusOK)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/info", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("业务路径 status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// 网关注入的租户头不被信任：即使存在也不能替代 JWT 校验。
func TestEnforce_GatewayTenantHeaderNotTrusted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gateway.Enforce(true, &stubAuthenticator{err: errors.New("always fail")}, nil, nil))
	r.GET("/api/v1/auth/info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/info", nil)
	req.Header.Set(gateway.HeaderTenant, "1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d（仅带租户头不足以放行）", w.Code, http.StatusUnauthorized)
	}
}