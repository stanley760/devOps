// Package response defines shared HTTP response contracts.
package response

// BaseResponse is the standard API response envelope.
type BaseResponse[T any] struct {
	Code int    `json:"code" doc:"业务码，与 HTTP 状态码一致" required:"true"`
	Msg  string `json:"msg" doc:"响应说明" required:"true"`
	Data T      `json:"data" doc:"响应数据" required:"true"`
}
