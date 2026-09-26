package humaparam_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"net/http/httptest"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"devops-infra/http/humaerr"
	"devops-infra/http/humaparam"
)

// optInput 带 Optional query 参数的输入（复现分页筛选的 *int8 语义）。
type optInput struct {
	Status humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1"`
}

type optOutput struct {
	Body struct {
		Status *int8 `json:"status"`
	}
}

func newOptAPI(t *testing.T) humatest.TestAPI {
	cfg := huma.DefaultConfig("p", "v1")
	cfg.CreateHooks = nil // 与生产 apidoc.New 对齐：禁用 $schema 注入
	_, api := humatest.New(t, cfg)
	registerOpt(api)
	return api
}

func registerOpt(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "opt", Method: http.MethodGet, Path: "/opt",
	}, func(ctx context.Context, in *optInput) (*optOutput, error) {
		out := &optOutput{}
		out.Body.Status = in.Status.Ptr()
		return out, nil
	})
}

func optStatus(t *testing.T, resp *httptest.ResponseRecorder) *int8 {
	t.Helper()
	var out struct {
		Status *int8 `json:"status"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应 JSON: %v", err)
	}
	return out.Status
}

func TestOptionalAbsent(t *testing.T) {
	api := newOptAPI(t)

	resp := api.Get("/opt")
	if resp.Code != http.StatusOK {
		t.Fatalf("缺省请求 status = %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	if s := optStatus(t, resp); s != nil {
		t.Fatalf("缺省 status 应为 nil, got %v", *s)
	}
}

func TestOptionalExplicitZero(t *testing.T) {
	api := newOptAPI(t)

	// 显式传 0（有效业务值）→ IsSet=true、Ptr() 指向 0
	resp := api.Get("/opt?status=0")
	if resp.Code != http.StatusOK {
		t.Fatalf("显式 0 status = %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	s := optStatus(t, resp)
	if s == nil || *s != 0 {
		t.Fatalf("显式 0 应 IsSet 且值 0, got %v", s)
	}
}

func TestOptionalExplicitOne(t *testing.T) {
	api := newOptAPI(t)

	resp := api.Get("/opt?status=1")
	if resp.Code != http.StatusOK {
		t.Fatalf("显式 1 status = %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	s := optStatus(t, resp)
	if s == nil || *s != 1 {
		t.Fatalf("显式 1 应 IsSet 且值 1, got %v", s)
	}
}

func TestOptionalConstraintRejected(t *testing.T) {
	humaerr.Install()
	api := newOptAPI(t)

	// 显式传 2 违反 maximum:1 → 422→400 信封
	resp := api.Get("/opt?status=2")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("越界 status = %d, want 400, body=%s", resp.Code, resp.Body.String())
	}
}
