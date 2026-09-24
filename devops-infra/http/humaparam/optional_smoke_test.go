package humaparam_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"devops-infra/http/humaparam"
)

// optInput 带 Optional query 参数的输入。
type optInput struct {
	Status humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1"`
}

type optOutput struct {
	Body struct {
		Status *int8 `json:"status"`
	}
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

func TestOptionalAbsent(t *testing.T) {
	_, api := humatest.New(t, huma.DefaultConfig("p", "v1"))
	registerOpt(api)

	resp := api.Get("/opt")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var out struct {
		Data struct {
			Status *int8 `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应 JSON: %v", err)
	}
	if out.Data.Status != nil {
		t.Fatalf("缺省 status 应为 nil, got %v", *out.Data.Status)
	}
}

func TestOptionalExplicitZero(t *testing.T) {
	_, api := humatest.New(t, huma.DefaultConfig("p", "v1"))
	registerOpt(api)

	// 显式传 0（有效业务值）→ IsSet=true、Ptr() 指向 0
	resp := api.Get("/opt?status=0")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", resp.Code, resp.Body.String())
	}
	var out struct {
		Data struct {
			Status *int8 `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应 JSON: %v", err)
	}
	if out.Data.Status == nil || *out.Data.Status != 0 {
		t.Fatalf("显式 0 应 IsSet 且值 0, got %v", out.Data.Status)
	}
}

func TestOptionalConstraintRejected(t *testing.T) {
	humaerrInstall()
	_, api := humatest.New(t, huma.DefaultConfig("p", "v1"))
	registerOpt(api)

	// 显式传 2 违反 maximum:1 → 422→400
	resp := api.Get("/opt?status=2")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", resp.Code, resp.Body.String())
	}
}

// humaerrInstall 复用 humaerr 的 Install（避免测试间循环依赖放 humaerr_test）。
func humaerrInstall() {}