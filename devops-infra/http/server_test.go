package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/stanley760/devops/devops-infra/config"
	"github.com/stanley760/devops/devops-infra/observability"
)

func TestReadyZReportsCheckStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, err := newRouter(config.ServerConfig{}, nil, []ReadinessCheck{
		{Name: "mysql", Check: func(context.Context) error { return nil }},
		{Name: "redis", Check: func(context.Context) error { return errBoom }},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Status  string   `json:"status"`
			Ready   []string `json:"ready"`
			Unready []string `json:"unready"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != http.StatusServiceUnavailable || body.Data.Status != "unready" {
		t.Fatalf("body = %+v", body)
	}
	if len(body.Data.Ready) != 1 || body.Data.Unready[0] != "redis" {
		t.Fatalf("checks = %+v", body.Data)
	}
}

func TestRouterDoesNotHandleCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, err := newRouter(config.ServerConfig{}, nil, nil,nil,nil)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	for header := range recorder.Header() {
		if strings.HasPrefix(header, "Access-Control-") {
			t.Fatalf("CORS header %q should be handled by Higress", header)
		}
	}
}

func TestRouterMountsObservabilityEndpointsAndMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	runtime, err := observability.Init(context.Background(), observability.Config{
		ServiceName:    "http-test",
		Env:            "test",
		MetricsEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Shutdown(shutdownCtx)
	}()

	router, err := newRouter(config.ServerConfig{}, func(r *gin.Engine) {
		r.GET("/users/:id", func(c *gin.Context) { c.Status(http.StatusTeapot) })
	}, nil, runtime)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("handler status = %d, want %d", recorder.Code, http.StatusTeapot)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "http_server_requests_total") ||
		!strings.Contains(body, `route="/users/:id"`) ||
		!strings.Contains(body, `status="418"`) {
		t.Fatalf("metrics missing route-template sample: %s", recorder.Body.String())
	}
}

var errBoom = &staticError{}

type staticError struct{}

func (*staticError) Error() string { return "boom" }
