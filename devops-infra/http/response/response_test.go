package response_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"devops-infra/errs"
	"devops-infra/http/response"
)

// run 经真实 gin 引擎执行 handler，返回其响应。
func run(t *testing.T, h gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	return w
}

// env 统一信封的宽松解析。
type env struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func TestOK(t *testing.T) {
	w := run(t, func(c *gin.Context) {
		response.OK(c, gin.H{"id": 1})
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var e env
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("响应不是统一信封: %v", err)
	}
	if e.Code != errs.CodeOK || e.Msg != "ok" {
		t.Fatalf("code=%d msg=%q, want code=200 msg=ok", e.Code, e.Msg)
	}
	if !strings.Contains(w.Body.String(), `"id":1`) {
		t.Fatalf("data 缺失: %s", w.Body.String())
	}
}

func TestOKPage(t *testing.T) {
	w := run(t, func(c *gin.Context) {
		response.OKPage(c, []int{1, 2}, 2)
	})

	var e struct {
		Code int `json:"code"`
		Data struct {
			List  []int `json:"list"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("响应不是统一信封: %v", err)
	}
	if e.Code != errs.CodeOK || len(e.Data.List) != 2 || e.Data.Total != 2 {
		t.Fatalf("分页信封不符合: %s", w.Body.String())
	}
}

func TestWrapBizError(t *testing.T) {
	w := run(t, response.Wrap(func(c *gin.Context) error {
		return errs.ErrNotFound
	}))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var e env
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("响应不是统一信封: %v", err)
	}
	if e.Code != errs.CodeNotFound || e.Msg != errs.ErrNotFound.Msg {
		t.Fatalf("信封不符合: %s", w.Body.String())
	}
}

func TestWrapWrappedBizError(t *testing.T) {
	// errors.As 需穿透 fmt.Errorf 的 %w 包装
	inner := errs.Wrap(errors.New("duplicate entry"), errs.CodeParam, "用户名已存在")
	w := run(t, response.Wrap(func(c *gin.Context) error {
		return fmt.Errorf("创建用户: %w", inner)
	}))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var e env
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("响应不是统一信封: %v", err)
	}
	if e.Msg != "用户名已存在" {
		t.Fatalf("msg = %q, want 用户名已存在", e.Msg)
	}
}

func TestWrapUnknownError(t *testing.T) {
	w := run(t, response.Wrap(func(c *gin.Context) error {
		return errors.New("db: connection refused")
	}))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "connection refused") {
		t.Fatalf("内部错误泄露到响应: %s", w.Body.String())
	}
	var e env
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("响应不是统一信封: %v", err)
	}
	if e.Code != errs.CodeInternal || e.Msg != errs.ErrInternal.Msg {
		t.Fatalf("信封不符合: %s", w.Body.String())
	}
}

func TestWrapMissingResponse(t *testing.T) {
	// handler 忘记写响应时兜底 500，避免请求挂起
	w := run(t, response.Wrap(func(c *gin.Context) error {
		return nil
	}))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestWrapSuccessPassThrough(t *testing.T) {
	// 已写响应且返回 nil：正常放行
	w := run(t, response.Wrap(func(c *gin.Context) error {
		response.OK(c, nil)
		return nil
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestFailCodeNormalization(t *testing.T) {
	// 误传 2xx 码时归一化为 500
	w := run(t, func(c *gin.Context) {
		response.Fail(c, 200, "误用成功码")
	})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// bindRun 模拟真实 handler：POST JSON → ShouldBindJSON 直返错误。
func bindRun(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/x", response.Wrap(func(c *gin.Context) error {
		var req struct {
			Name string `json:"name" binding:"required"`
			Age  int    `json:"age" binding:"gte=0"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			return err
		}
		response.OK(c, nil)
		return nil
	}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	return w
}

func TestWrapValidationErrorAs400(t *testing.T) {
	// 缺 required 字段：应 400 且 msg 带字段名，而非 500 兜底
	w := bindRun(t, `{"age":1}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var e env
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != errs.CodeParam || !strings.Contains(e.Msg, "Name") {
		t.Fatalf("code=%d msg=%q，应含字段名 Name 的 400 提示", e.Code, e.Msg)
	}
}

func TestWrapJSONSyntaxErrorAs400(t *testing.T) {
	w := bindRun(t, `{bad json`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var e env
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != errs.CodeParam {
		t.Fatalf("code=%d, want %d", e.Code, errs.CodeParam)
	}
}
