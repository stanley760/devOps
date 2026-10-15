package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/stanley760/devops/devops-infra/security/gateway"
)

const secret = "shared-secret-value"

// newEngine 装配一条最小链路：守卫 -> 业务 handler。
func newEngine(t *testing.T, secret string, exemptPaths, exemptPrefixes []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gateway.Enforce(secret, exemptPaths, exemptPrefixes))
	r.GET("/api/v1/auth/info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

func do(t *testing.T, r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set(gateway.HeaderToken, token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 经网关的正常请求（携带正确 token）必须放行。
func TestEnforce_ValidTokenPasses(t *testing.T) {
	r := newEngine(t, secret, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", secret); w.Code != http.StatusOK {
		t.Fatalf("合法请求被拒: status=%d body=%s", w.Code, w.Body.String())
	}
}

// 直连（无 token）必须 403 —— 这是网关旁路的核心防线。
func TestEnforce_MissingTokenRejected(t *testing.T) {
	r := newEngine(t, secret, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", ""); w.Code != http.StatusForbidden {
		t.Fatalf("直连请求未被拦截: status=%d", w.Code)
	}
}

// 错误 token 必须 403。
func TestEnforce_WrongTokenRejected(t *testing.T) {
	r := newEngine(t, secret, nil, nil)
	if w := do(t, r, "/api/v1/auth/info", "wrong"); w.Code != http.StatusForbidden {
		t.Fatalf("错误 token 未被拦截: status=%d", w.Code)
	}
}

// 空 secret 时守卫直通，保证未启用的环境零改动（不会因漏配密钥导致全站 403）。
func TestEnforce_EmptySecretPassthrough(t *testing.T) {
	r := newEngine(t, "", nil, nil)
	if w := do(t, r, "/api/v1/auth/info", ""); w.Code != http.StatusOK {
		t.Fatalf("未启用守卫时未直通: status=%d", w.Code)
	}
}

// 放行名单：健康检查等不经网关的端点仍可直连。
func TestEnforce_ExemptPaths(t *testing.T) {
	r := newEngine(t, secret, []string{"/healthz", "/readyz"}, nil)
	for _, p := range []string{"/healthz", "/readyz"} {
		if w := do(t, r, p, ""); w.Code != http.StatusOK {
			t.Fatalf("放行端点 %s 被拒: status=%d", p, w.Code)
		}
	}
	if w := do(t, r, "/api/v1/auth/info", ""); w.Code != http.StatusForbidden {
		t.Fatalf("非放行端点未被拦截: status=%d", w.Code)
	}
}

// 前缀放行用于 /apidoc 之类的挂载点。
func TestEnforce_ExemptPrefixes(t *testing.T) {
	r := newEngine(t, secret, nil, []string{"/apidoc"})
	if w := do(t, r, "/apidoc/openapi.json", ""); w.Code != http.StatusOK {
		t.Fatalf("前缀放行端点被拒: status=%d", w.Code)
	}
	// 前缀边界：不能被相似路径蹭过
	if w := do(t, r, "/apidocx", ""); w.Code != http.StatusForbidden {
		t.Fatalf("前缀越界路径未被拦截: status=%d", w.Code)
	}
}

// 拒绝时必须返回统一响应信封，不泄露「头缺失」与「头错误」的区别。
func TestEnforce_ErrorEnvelope(t *testing.T) {
	r := newEngine(t, secret, nil, nil)
	w := do(t, r, "/api/v1/auth/info", "")
	var body struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("拒绝时未返回统一信封: %v body=%s", err, w.Body.String())
	}
	if body.Code != http.StatusForbidden || body.Msg != "请求必须经由网关访问" {
		t.Fatalf("信封不符合: %s", w.Body.String())
	}
}

// EnforceFromConfig：RequireToken=false 时直通，即使配了 token 也不拦。
func TestEnforceFromConfig_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gateway.EnforceFromConfig(gateway.Config{RequireToken: false, Token: secret}, nil, nil))
	r.GET("/x", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	if w := do(t, r, "/x", ""); w.Code != http.StatusOK {
		t.Fatalf("未启用时未直通: status=%d", w.Code)
	}
}

// EnforceFromConfig：启用后生效，且配置追加的 exempt_paths 生效。
func TestEnforceFromConfig_EnabledWithExempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gateway.EnforceFromConfig(
		gateway.Config{RequireToken: true, Token: secret, ExemptPaths: []string{"/healthz"}},
		[]string{"/readyz"}, nil,
	))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/api/x", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	for _, p := range []string{"/healthz", "/readyz"} {
		if w := do(t, r, p, ""); w.Code != http.StatusOK {
			t.Fatalf("放行端点 %s 被拒: status=%d", p, w.Code)
		}
	}
	if w := do(t, r, "/api/x", ""); w.Code != http.StatusForbidden {
		t.Fatalf("业务端点未被拦截: status=%d", w.Code)
	}
	if w := do(t, r, "/api/x", secret); w.Code != http.StatusOK {
		t.Fatalf("合法请求被拒: status=%d", w.Code)
	}
}
