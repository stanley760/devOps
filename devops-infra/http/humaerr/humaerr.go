// Package humaerr huma 错误出口与统一信封对齐（D1/D6/D7）。
//
// huma 默认错误响应为 RFC7807 problem+json，与项目的 BaseResponse 信封契约不符。
// Install() 覆盖包级变量 huma.NewError 后（须在任何 huma 请求处理前调用）：
//   - handler 返回 errs.ToHumaError(err) → 走 NewError → 信封 ✔
//   - huma schema 校验失败（422）→ 映射 400，校验明细并入 msg ✔
//   - 自定义 Envelope 未实现 ContentTypeFilter，Content-Type 保持 application/json ✔
//
// WriteErr 会经 NewErrorWithContext（默认委托 NewError）取错误对象，
// 并以 GetStatus() 重读状态码——Install 内的状态映射因此对校验路径全局生效。
package humaerr

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Envelope 错误信封（与 response.BaseResponse[any] 的失败形态字节一致）。
type Envelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"` // 恒 nil，序列化为 "data":null，与 gin 版一致
}

// GetStatus 实现 huma.StatusError。
func (e *Envelope) GetStatus() int { return e.Code }

// Error 实现 error / huma.StatusError。
func (e *Envelope) Error() string { return e.Msg }

// Install 覆盖 huma.NewError（包级变量），全局生效；在 main 启动时调用。
// 422（huma schema 校验失败）映射为 400，校验明细并入 msg，与旧 gin 契约一致。
func Install() {
	huma.NewError = func(status int, msg string, detailErrs ...error) huma.StatusError {
		if status == http.StatusUnprocessableEntity {
			status = http.StatusBadRequest
			msg = formatValidation(msg, detailErrs)
		}
		if msg == "" {
			msg = http.StatusText(status)
		}
		return &Envelope{Code: status, Msg: msg}
	}
}

// formatValidation 汇总校验明细为面向用户的中文提示。
func formatValidation(msg string, errs []error) string {
	if len(errs) == 0 {
		return "参数校验失败: " + msg
	}
	var b strings.Builder
	b.WriteString("参数校验失败: ")
	for i, err := range errs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(err.Error())
	}
	return b.String()
}

// envelopeSchema 错误信封 schema（文档形态与运行时输出一致）。
func envelopeSchema() *huma.Schema {
	return &huma.Schema{
		Type: "object",
		Properties: map[string]*huma.Schema{
			"code": {Type: "integer", Description: "业务码，与 HTTP 状态码一致"},
			"msg":  {Type: "string", Description: "提示信息"},
			"data": {Description: "业务数据，失败时恒为 null"},
		},
	}
}

// ErrResponses 生成指定状态码的信封响应声明（D6）。
// 与 Operation.Responses 搭配，替代 huma 默认 problem+json 错误文档。
func ErrResponses(codes map[string]string) map[string]*huma.Response {
	content := map[string]*huma.MediaType{
		"application/json": {Schema: envelopeSchema()},
	}
	responses := make(map[string]*huma.Response, len(codes))
	for code, desc := range codes {
		responses[code] = &huma.Response{Description: desc, Content: content}
	}
	return responses
}

// CommonErrResponses 预置常用错误码（400/401/403/404/500）的信封响应声明。
func CommonErrResponses() map[string]*huma.Response {
	return ErrResponses(map[string]string{
		"400": "参数错误",
		"401": "未登录或会话已过期",
		"403": "无操作权限",
		"404": "资源不存在",
		"500": "服务器内部错误",
	})
}
