package humaerr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/stanley760/devOps/devops-infra/errs"
	"github.com/stanley760/devOps/devops-infra/http/humaerr"
)

// smokeInput 冒烟用输入：name 有 minLength 约束。
type smokeInput struct {
	Body struct {
		Name string `json:"name" minLength:"2"`
	}
}

// smokeOutput 冒烟用输出。
type smokeOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

// newTestAPI 与生产 apidoc.New 对齐：清空 CreateHooks 禁用 $schema 注入，
// 保证信封字节级一致（SchemaLinkTransformer 会向响应加 $schema 字段与 Link 头）。
func newTestAPI(t *testing.T, title string) humatest.TestAPI {
	t.Helper()
	cfg := huma.DefaultConfig(title, "v1")
	cfg.CreateHooks = nil
	_, api := humatest.New(t, cfg)
	return api
}

func TestInstallValidation422To400Envelope(t *testing.T) {
	humaerr.Install()
	api := newTestAPI(t, "smoke")

	huma.Register(api, huma.Operation{
		OperationID: "smoke", Method: http.MethodPost, Path: "/smoke",
	}, func(ctx context.Context, in *smokeInput) (*smokeOutput, error) {
		return nil, errs.New(errs.CodeForbidden, "不可达")
	})

	// minLength 违反 → huma 内部 422 → Install 映射 400 信封
	resp := api.Post("/smoke", map[string]any{"name": "x"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("校验失败状态 = %d, want 400（422 已映射）, body=%s", resp.Code, resp.Body.String())
	}
	var env humaerr.Envelope
	if err := json.Unmarshal(resp.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是信封 JSON: %v, body=%s", err, resp.Body.String())
	}
	if env.Code != 400 {
		t.Fatalf("信封 code = %d, want 400", env.Code)
	}
	if !strings.Contains(env.Msg, "参数校验失败") {
		t.Fatalf("信封 msg 缺少前缀: %q", env.Msg)
	}
	if env.Data != nil {
		t.Fatalf("信封 data 应为 null: %v", env.Data)
	}
}

func TestInstallBizErrorEnvelope(t *testing.T) {
	humaerr.Install()
	api := newTestAPI(t, "smoke2")

	huma.Register(api, huma.Operation{
		OperationID: "biz", Method: http.MethodPost, Path: "/biz",
	}, func(ctx context.Context, _ *smokeInput) (*smokeOutput, error) {
		return nil, errs.ToHumaError(errs.New(errs.CodeForbidden, "无操作权限"))
	})

	// 合法 body + handler 返回 BizError → 403 信封（经 errs.ToHumaError → NewError）
	resp := api.Post("/biz", map[string]any{"name": "ab"})
	if resp.Code != http.StatusForbidden {
		t.Fatalf("BizError 状态 = %d, want 403, body=%s", resp.Code, resp.Body.String())
	}
	var env humaerr.Envelope
	if err := json.Unmarshal(resp.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是信封 JSON: %v", err)
	}
	if env.Code != 403 || env.Msg != "无操作权限" {
		t.Fatalf("信封不符: %+v", env)
	}
}

func TestInstallContentTypeJSON(t *testing.T) {
	humaerr.Install()
	api := newTestAPI(t, "smoke3")

	huma.Register(api, huma.Operation{
		OperationID: "ct", Method: http.MethodPost, Path: "/ct",
	}, func(ctx context.Context, _ *smokeInput) (*smokeOutput, error) {
		return nil, errs.ToHumaError(errs.New(errs.CodeInternal, "内部错误"))
	})

	resp := api.Post("/ct", map[string]any{"name": "ab"})
	ct := resp.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json（无 ContentTypeFilter 不应协商为 problem+json）", ct)
	}
	// 契约保护：响应不得被 SchemaLinkTransformer 注入 $schema 字段
	if strings.Contains(resp.Body.String(), "$schema") {
		t.Fatalf("响应被注入 $schema，破坏信封字节契约: %s", resp.Body.String())
	}
}
