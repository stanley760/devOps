package apidoc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	apidoc "devops-infra/http/apidoc"
)

func newEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// pingOutput ping 输出。
type pingOutput struct {
	Body struct {
		Pong bool `json:"pong" doc:"是否存活"`
	}
}

// registerPing 在文档上注册一个最小操作，用于验证业务注册可见。
func registerPing(t *testing.T, api huma.API) {
	t.Helper()
	huma.Register(api, huma.Operation{
		OperationID: "ping",
		Method:      http.MethodGet,
		Path:        "/ping",
		Summary:     "健康探测",
		Tags:        []string{"test"},
	}, func(ctx context.Context, _ *pingInput) (*pingOutput, error) {
		out := &pingOutput{}
		out.Body.Pong = true
		return out, nil
	})
}

// pingInput ping 输入（无参数）。
type pingInput struct{}

func TestNew_DefaultPaths(t *testing.T) {
	r := newEngine()
	api := apidoc.New(r, apidoc.Options{Title: "test API", Version: "v1"})
	registerPing(t, api)

	// 文档 UI 可达
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apidoc", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("docs UI: got %d, want 200", w.Code)
	}

	// openapi.json 可达
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apidoc/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("openapi.json: got %d, want 200", w.Code)
	}

	// 业务注册的操作出现在 OpenAPI 文档中
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi.json 不是合法 JSON: %v", err)
	}
	paths, _ := doc["paths"].(map[string]any)
	if _, ok := paths["/ping"]; !ok {
		t.Fatalf("openapi.json 缺少已注册操作 /ping，paths=%v", paths)
	}
	// 标题与版本
	info, _ := doc["info"].(map[string]any)
	if info["title"] != "test API" || info["version"] != "v1" {
		t.Fatalf("info 不符: %v", info)
	}
}

func TestNew_DocJSONAlias(t *testing.T) {
	r := newEngine()
	_ = apidoc.New(r, apidoc.Options{})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apidoc/doc.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("doc.json alias: got %d, want 200", w.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("doc.json 不是合法 JSON: %v", err)
	}
}

func TestNew_CustomDocsPath(t *testing.T) {
	r := newEngine()
	api := apidoc.New(r, apidoc.Options{DocsPath: "/api-docs"})
	registerPing(t, api)

	for _, path := range []string{"/api-docs", "/api-docs/openapi.json", "/api-docs/doc.json"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d, want 200", path, w.Code)
		}
	}
}

func TestNew_BasePathServer(t *testing.T) {
	r := newEngine()
	_ = apidoc.New(r, apidoc.Options{BasePath: "https://ops.example.com"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apidoc/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("openapi.json: got %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "https://ops.example.com") {
		t.Fatalf("openapi.json servers 未包含 BasePath: %s", w.Body.String())
	}
}

func TestNew_BearerAuth(t *testing.T) {
	r := newEngine()
	_ = apidoc.New(r, apidoc.Options{
		BearerAuth: true,
		Security:   []map[string][]string{{"bearerAuth": {}}},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apidoc/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("openapi.json: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "bearerAuth") {
		t.Fatalf("openapi.json 未声明 bearerAuth 安全方案")
	}
	if !strings.Contains(body, `"bearerFormat":"JWT"`) && !strings.Contains(body, `"bearerFormat": "JWT"`) {
		t.Fatalf("openapi.json 未声明 JWT bearerFormat")
	}
}

func TestMount_Deprecated(t *testing.T) {
	r := newEngine()
	_ =  apidoc.New(r, apidoc.Options{Title: "mounted"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/apidoc/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("openapi.json: got %d, want 200", w.Code)
	}
}

func TestDefaults_Fill(t *testing.T) {
	got := apidoc.Options{}.Defaults()
	if got.Title != "API" || got.Version != "v1" || got.DocsPath != "/apidoc" {
		t.Fatalf("Defaults 填充不符: %+v", got)
	}
}