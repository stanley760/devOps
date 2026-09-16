package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

var errBoom = &staticError{}

type staticError struct{}

func (*staticError) Error() string { return "boom" }
