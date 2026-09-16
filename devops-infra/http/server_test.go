package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"devops-infra/config"
)

func TestReadyZReportsCheckStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, err := newRouter(config.ServerConfig{}, nil, []ReadinessCheck{
		{Name: "mysql", Check: func(context.Context) error { return nil }},
		{Name: "redis", Check: func(context.Context) error { return errBoom }},
	})
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
	router, err := newRouter(config.ServerConfig{}, nil, nil)
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

var errBoom = &staticError{}

type staticError struct{}

func (*staticError) Error() string { return "boom" }
