// Package errs 定义统一错误码与业务错误类型。
//
// 统一错误处理的数据基础：
//   - 错误码与 HTTP 状态码同空间：200 成功，400/401/403/404/500 等失败
//   - 业务错误统一用 *BizError 携带 code 与用户可读 msg，可再包装底层错误
//   - handler / service 以返回 error 的方式报告失败，
//     由 response.FailErr 识别 BizError 并转换成统一响应
package errs

import (
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
)

// 错误码常量，与 HTTP 状态码对齐。
const (
	CodeOK               = 200 // 成功
	CodeParam            = 400 // 参数错误
	CodeUnauthorized     = 401 // 未登录或会话已过期
	CodeForbidden        = 403 // 无操作权限
	CodeNotFound         = 404 // 资源不存在
	CodeMethodNotAllowed = 405 // 请求方法不支持
	CodeInternal         = 500 // 服务器内部错误
)

// BizError 业务错误。
// Code 面向响应码与前端分支判断，Msg 面向用户展示，Err 保留底层原因供日志排查。
type BizError struct {
	Code int
	Msg  string
	Err  error
}

// Error 实现 error 接口。
func (e *BizError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("biz error: code=%d msg=%q: %v", e.Code, e.Msg, e.Err)
	}
	return fmt.Sprintf("biz error: code=%d msg=%q", e.Code, e.Msg)
}

// Unwrap 支持 errors.Is / errors.As 穿透到底层错误。
func (e *BizError) Unwrap() error { return e.Err }

// New 新建业务错误。
func New(code int, msg string) *BizError {
	return &BizError{Code: code, Msg: msg}
}

// Wrap 在底层错误之上附加业务语义，保留原始错误用于日志。
func Wrap(err error, code int, msg string) *BizError {
	return &BizError{Code: code, Msg: msg, Err: err}
}

// ToHumaError 将统一业务错误转换为 Huma Problem Details 错误。
//
// 只透出面向用户的 BizError.Msg，底层 Err 不进入响应，避免数据库凭据、
// SQL、内网地址等敏感信息泄露；非 BizError 一律按内部错误处理。
func ToHumaError(err error) error {
	if err == nil {
		return nil
	}

	var bizErr *BizError
	if !errors.As(err, &bizErr) {
		return huma.Error500InternalServerError("服务器内部错误")
	}

	status := bizErr.Code
	if status < 400 || status > 599 {
		status = CodeInternal
	}
	message := bizErr.Msg
	if message == "" {
		message = "服务器内部错误"
	}
	return huma.NewError(status, message)
}

// 常用预定义错误，可直接返回或用 errors.Is 比较。
var (
	ErrParam            = New(CodeParam, "参数错误")
	ErrUnauthorized     = New(CodeUnauthorized, "未登录或会话已过期")
	ErrForbidden        = New(CodeForbidden, "无操作权限")
	ErrNotFound         = New(CodeNotFound, "资源不存在")
	ErrMethodNotAllowed = New(CodeMethodNotAllowed, "请求方法不支持")
	ErrInternal         = New(CodeInternal, "服务器内部错误")
)
