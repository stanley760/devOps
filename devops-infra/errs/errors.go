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
	"log/slog"
	"runtime"
	"strings"

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
// Code 面向响应码与前端分支判断，Msg 面向用户展示，Err 保留底层原因供日志排查，
// stack 为错误创建点的调用栈（New/Wrap 时捕获），日志经 Stack() 渲染定位错误发生位置。
type BizError struct {
	Code  int
	Msg   string
	Err   error
	stack []uintptr
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

// Stack 渲染错误创建点的调用栈（每帧两行：函数名 + 文件:行号）。
// 零值构造或未捕获（理论上不应出现）时返回空串，调用方据此省略日志字段。
func (e *BizError) Stack() string {
	return formatFrames(e.stack)
}

// New 新建业务错误。
func New(code int, msg string) *BizError {
	return &BizError{Code: code, Msg: msg, stack: callers()}
}

// Wrap 在底层错误之上附加业务语义，保留原始错误用于日志。
func Wrap(err error, code int, msg string) *BizError {
	return &BizError{Code: code, Msg: msg, Err: err, stack: callers()}
}

// callers 捕获调用栈，跳过 errs 包自身的帧（runtime.Callers/callers/New/Wrap），
// 使栈顶落在业务代码的错误创建处。
func callers() []uintptr {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	return pcs[:n]
}

// formatFrames 渲染调用栈（每帧两行：函数名 + 文件:行号）；空栈返回空串。
func formatFrames(pcs []uintptr) string {
	if len(pcs) == 0 {
		return ""
	}
	var b strings.Builder
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		fmt.Fprintf(&b, "%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
	}
	return b.String()
}

// ToHumaError 将统一业务错误转换为 Huma Problem Details 错误。
//
// 只透出面向用户的 BizError.Msg，底层 Err 不进入响应，避免数据库凭据、
// SQL、内网地址等敏感信息泄露；非 BizError 一律按内部错误处理。
//
// 响应隐藏细节的同时，服务端日志必须保留完整错误链与发生点堆栈：
// 非 BizError 与 5xx BizError 在此记录 Error 日志（这是 huma 路径唯一的
// 错误日志出口）；4xx 属预期业务拒绝，不记日志（访问日志已有 Debug 记录）。
func ToHumaError(err error) error {
	if err == nil {
		return nil
	}

	var bizErr *BizError
	if !errors.As(err, &bizErr) {
		slog.Error("未分类错误（非 BizError，按 500 响应）",
			"err", err, "stack", formatFrames(callers()))
		return huma.Error500InternalServerError("服务器内部错误")
	}

	if bizErr.Code >= 500 {
		slog.Error("业务错误（5xx）",
			"code", bizErr.Code, "msg", bizErr.Msg, "err", err, "stack", bizErr.Stack())
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
