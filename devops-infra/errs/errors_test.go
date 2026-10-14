package errs_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/stanley760/devops/devops-infra/errs"
)

func TestToHumaErrorMapsBizStatusAndMessage(t *testing.T) {
	got := errs.ToHumaError(errs.ErrForbidden)

	var statusErr huma.StatusError
	if !errors.As(got, &statusErr) {
		t.Fatalf("ToHumaError() = %T, want huma.StatusError", got)
	}
	if statusErr.GetStatus() != errs.CodeForbidden {
		t.Fatalf("status = %d, want %d", statusErr.GetStatus(), errs.CodeForbidden)
	}
	if got.Error() != errs.ErrForbidden.Msg {
		t.Fatalf("message = %q, want %q", got.Error(), errs.ErrForbidden.Msg)
	}
}

func TestToHumaErrorNormalizesInvalidStatus(t *testing.T) {
	got := errs.ToHumaError(errs.New(200, "invalid success response"))

	var statusErr huma.StatusError
	if !errors.As(got, &statusErr) {
		t.Fatalf("ToHumaError() = %T, want huma.StatusError", got)
	}
	if statusErr.GetStatus() != errs.CodeInternal {
		t.Fatalf("status = %d, want %d", statusErr.GetStatus(), errs.CodeInternal)
	}
}

func TestToHumaErrorHidesUnknownErrorDetail(t *testing.T) {
	cause := errors.New("database password rejected")
	got := errs.ToHumaError(cause)

	var statusErr huma.StatusError
	if !errors.As(got, &statusErr) {
		t.Fatalf("ToHumaError() = %T, want huma.StatusError", got)
	}
	if statusErr.GetStatus() != errs.CodeInternal {
		t.Fatalf("status = %d, want %d", statusErr.GetStatus(), errs.CodeInternal)
	}
	if strings.Contains(got.Error(), cause.Error()) {
		t.Fatalf("unknown error must not leak detail: %q", got.Error())
	}
}

func TestToHumaErrorNil(t *testing.T) {
	if got := errs.ToHumaError(nil); got != nil {
		t.Fatalf("ToHumaError(nil) = %v, want nil", got)
	}
}

// captureSlog 把默认 slog 输出重定向到缓冲区，返回还原函数。
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	origin := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	return &buf, func() { slog.SetDefault(origin) }
}

func TestWrapCapturesCallerStack(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	got := errs.Wrap(cause, errs.CodeInternal, "发号失败")

	stack := got.Stack()
	if !strings.Contains(stack, "TestWrapCapturesCallerStack") {
		t.Fatalf("Stack() 未落在业务调用点:\n%s", stack)
	}
	if strings.Contains(stack, "devops-infra/errs.") {
		t.Fatalf("Stack() 不应包含 errs 包自身帧:\n%s", stack)
	}
	if !strings.Contains(got.Error(), cause.Error()) {
		t.Fatalf("Error() 应保留底层原因: %q", got.Error())
	}
}

func TestStackOfZeroValueBizErrorIsEmpty(t *testing.T) {
	if got := (&errs.BizError{}).Stack(); got != "" {
		t.Fatalf("零值 BizError Stack() = %q, want 空串", got)
	}
}

func TestToHumaErrorLogsUnclassifiedWithStack(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	cause := errors.New("sql: connection refused")
	got := errs.ToHumaError(cause)

	var statusErr huma.StatusError
	if !errors.As(got, &statusErr) || statusErr.GetStatus() != errs.CodeInternal {
		t.Fatalf("ToHumaError(非 BizError) 应为 500, got %v", got)
	}
	if log := buf.String(); !strings.Contains(log, "未分类错误") ||
		!strings.Contains(log, cause.Error()) ||
		!strings.Contains(log, "TestToHumaErrorLogsUnclassifiedWithStack") {
		t.Fatalf("未分类错误日志应含错误链与调用点堆栈, got:\n%s", log)
	}
}

func TestToHumaErrorLogs5xxBizErrorWithCauseAndStack(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	cause := errors.New("Error 1062: Duplicate entry")
	_ = errs.ToHumaError(errs.Wrap(cause, errs.CodeInternal, "创建用户失败"))

	log := buf.String()
	if !strings.Contains(log, "业务错误（5xx）") ||
		!strings.Contains(log, cause.Error()) ||
		!strings.Contains(log, "TestToHumaErrorLogs5xxBizErrorWithCauseAndStack") {
		t.Fatalf("5xx 日志应含业务语义、底层原因与堆栈, got:\n%s", log)
	}
}

func TestToHumaErrorSkips4xxLogging(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	_ = errs.ToHumaError(errs.New(errs.CodeParam, "用户名已存在"))

	if log := buf.String(); log != "" {
		t.Fatalf("4xx 属预期业务拒绝, 不应记错误日志, got:\n%s", log)
	}
}
