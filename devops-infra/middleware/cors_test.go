package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func corsRouter(cfg CORSConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS(cfg))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}

func TestCORSPreflightAllowed(t *testing.T) {
	r := corsRouter(CORSConfig{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("预检应 204，got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("应回显命中源，got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials 场景应回显 true，got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("应回显 Allow-Methods")
	}
	if got := w.Header().Get("Access-Control-Max-Age"); got != "43200" {
		t.Fatalf("MaxAge=12h 应为 43200 秒，got %q", got)
	}
}

func TestCORSPreflightNotAllowed(t *testing.T) {
	r := corsRouter(CORSConfig{AllowOrigins: []string{"http://localhost:5173"}})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("预检仍应 204 结束，got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("未放行源不应有 ACAO 头，got %q", got)
	}
}

func TestCORSSimpleRequest(t *testing.T) {
	r := corsRouter(CORSConfig{AllowOrigins: []string{"http://localhost:5173"}})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || w.Body.String() != "pong" {
		t.Fatalf("简单请求应穿透到业务，got %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("应回显命中源，got %q", got)
	}
}

func TestCORSSimpleRequestNoOrigin(t *testing.T) {
	r := corsRouter(CORSConfig{AllowOrigins: []string{"*"}})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil) // 无 Origin 头
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("无 Origin 为同源请求应直接穿透，got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("同源请求不应加 CORS 头，got %q", got)
	}
}

func TestCORSWildcard(t *testing.T) {
	r := corsRouter(CORSConfig{AllowOrigins: []string{"*"}})

	// 通配 + 无 credentials：回显 *
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "http://any.example.com")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("通配非 credentials 应回显 *，got %q", got)
	}

	// 通配 + credentials：规范禁止 *，回显具体源
	r2 := corsRouter(CORSConfig{AllowOrigins: []string{"*"}, AllowCredentials: true})
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req2.Header.Set("Origin", "http://any.example.com")
	r2.ServeHTTP(w2, req2)
	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "http://any.example.com" {
		t.Fatalf("通配 + credentials 应回显具体源，got %q", got)
	}
}
