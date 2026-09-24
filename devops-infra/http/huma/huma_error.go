package huma

import (
	"devops-infra/errs"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type HumanErr struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

func (e *HumanErr) GetStatus() int {
		return e.Code
	}

func (e *HumanErr) Error() string {
	return e.Msg
}

func Install() {
	huma.NewError = func(status int, msg string, detailErrs ...error) huma.StatusError {
		if status == http.StatusUnprocessableEntity {
			status = errs.CodeInternal
			msg = formatValidation(msg, detailErrs)
		}
		return &HumanErr{
			Code: status,
			Msg:  msg,
		}
	}
}

func formatValidation(msg string, bizErrs []error) string {
	if msg == "" {
		msg = "系统错误"
	}
	for _, err := range bizErrs {
		msg += fmt.Sprintf(" %s", err.Error())
	}
	return msg
}