// huma 出口信封 helper（D4/D7）。
//
// huma handler 的 Output.Body 直接复用这些构造函数，成功形态与
// gin 版 OK/OKPage 字节一致；WriteHumaEnvelope 供中间件终止路径直写信封。
package response

import (
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"devops-infra/errs"
)

// OKBody 成功响应体（huma handler Output.Body 用）。
func OKBody[T any](data T) BaseResponse[T] {
	return BaseResponse[T]{Code: errs.CodeOK, Msg: "ok", Data: data}
}

// EmptyBody 无数据成功响应体：Data 为 nil any，序列化为 "data":null（与 OK(c, nil) 字节一致）。
func EmptyBody() BaseResponse[any] {
	return BaseResponse[any]{Code: errs.CodeOK, Msg: "ok", Data: nil}
}

// PageBody 分页成功响应体（与 OKPage 字节一致）。
func PageBody[T any](list T, total int64) BaseResponse[PageData[T]] {
	return BaseResponse[PageData[T]]{
		Code: errs.CodeOK, Msg: "ok",
		Data: PageData[T]{List: list, Total: total},
	}
}

// WriteHumaEnvelope 向 huma 上下文直写失败信封（中间件 401/403 等终止路径用）。
// 仅放行 4xx/5xx，避免误传 2xx 造成语义混乱。
func WriteHumaEnvelope(hctx huma.Context, status int, msg string) {
	if status < 400 || status > 599 {
		status = errs.CodeInternal
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	body, _ := json.Marshal(BaseResponse[any]{Code: status, Msg: msg})
	hctx.SetHeader("Content-Type", "application/json; charset=utf-8")
	hctx.SetStatus(status)
	_, _ = hctx.BodyWriter().Write(body)
}
