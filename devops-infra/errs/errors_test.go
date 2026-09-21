package errs_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"devops-infra/errs"
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
