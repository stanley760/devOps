// Package response 统一 HTTP 响应格式与错误出口。
//
// 所有业务接口返回 BaseResponse 信封：
//
//	{"code": 200, "msg": "ok", "data": {...}}
//
// code 与 HTTP 状态码一致。业务 handler 以返回 error 的方式报告失败，
// 经 Wrap 适配后由 FailErr 统一转换，避免各处手写错误响应：
//
//	r.GET("/api/v1/users", response.Wrap(h.ListUsers))
//
// handler 内部成功路径自行调用 OK / OKPage 写出响应。
package response

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/stanley760/devOps/devops-infra/errs"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// BaseResponse 统一响应结构。
// T 为 data 的实际类型；通用出口使用 BaseResponse[any]，
// 强类型场景（如代码生成）可自行实例化。
type BaseResponse[T any] struct {
	Code int    `json:"code"` // 业务码，与 HTTP 状态码一致
	Msg  string `json:"msg"`  // 提示信息
	Data T      `json:"data"` // 业务数据，无数据时为 null
}

// PageData 分页数据信封。
type PageData[T any] struct {
	List  T     `json:"list"`  // 当前页数据
	Total int64 `json:"total"` // 总条数
}

// Handler 业务 handler 签名：以返回 error 报告失败，
// 成功时自行调用 OK / OKPage 写响应。
type Handler func(c *gin.Context) error

// OK 成功响应。data 传 nil 表示无数据（删除、修改类操作）。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, BaseResponse[any]{
		Code: errs.CodeOK,
		Msg:  "ok",
		Data: data,
	})
}

// OKPage 分页成功响应。
func OKPage(c *gin.Context, list any, total int64) {
	c.JSON(http.StatusOK, BaseResponse[PageData[any]]{
		Code: errs.CodeOK,
		Msg:  "ok",
		Data: PageData[any]{List: list, Total: total},
	})
}

// Fail 失败响应，code 同时作为 HTTP 状态码。
// 仅放行 4xx/5xx，避免误传 2xx 造成语义混乱。
func Fail(c *gin.Context, code int, msg string) {
	if code < 400 || code > 599 {
		code = errs.CodeInternal
	}
	if msg == "" {
		msg = http.StatusText(code)
	}
	c.AbortWithStatusJSON(code, BaseResponse[any]{Code: code, Msg: msg})
}

// FailErr 错误统一出口：识别 *errs.BizError 使用其 code/msg，
// 其余未知错误记日志并按 500 处理，不向客户端泄露内部细节。
// BizError 属预期内的业务失败（如参数错误），不再记日志。
func FailErr(c *gin.Context, err error) {
	var be *errs.BizError
	if errors.As(err, &be) {
		Fail(c, be.Code, be.Msg)
		return
	}

	// handler 习惯直接 return c.ShouldBindJSON 的错误，
	// 绑定/校验类错误属客户端问题，归类 400 而非 500 兜底
	if msg, ok := asParamError(err); ok {
		Fail(c, errs.CodeParam, msg)
		return
	}

	slog.Error("未分类错误", "err", err, "method", c.Request.Method, "path", c.FullPath())
	Fail(c, errs.CodeInternal, errs.ErrInternal.Msg)
}

// asParamError 识别请求绑定/校验类错误，返回面向用户的提示。
func asParamError(err error) (string, bool) {
	var valErrs validator.ValidationErrors
	if errors.As(err, &valErrs) {
		var b strings.Builder
		for i, fe := range valErrs {
			if i > 0 {
				b.WriteString("; ")
			}
			fmt.Fprintf(&b, "字段 %s 未通过 %s 校验", fe.Field(), fe.Tag())
		}
		return "参数校验失败: " + b.String(), true
	}
	var synErr *json.SyntaxError
	if errors.As(err, &synErr) {
		return "请求体不是合法 JSON", true
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return "请求体字段类型错误", true
	}
	var parseErr *time.ParseError
	if errors.As(err, &parseErr) {
		return "请求体时间格式错误", true
	}
	if errors.Is(err, io.EOF) {
		return "请求体不能为空", true
	}
	return "", false
}

// Wrap 把 Handler 适配为 gin.HandlerFunc，并统一处理错误：
//   - 返回 BizError（含被 fmt.Errorf("%w") 包装的）→ 按其 code/msg 响应
//   - 返回绑定/校验类错误（ShouldBindJSON 直返）→ 400 带字段级提示
//   - 返回其他错误 → 500，细节只进日志
//   - 返回 nil 但未写响应（漏写 bug）→ 兜底 500，避免请求挂起
//   - 已写响应又返回错误 → 无法覆盖输出，记日志后中断
func Wrap(h Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := h(c)
		switch {
		case err == nil:
			if !c.Writer.Written() {
				slog.Error("handler 未写响应", "method", c.Request.Method, "path", c.FullPath())
				Fail(c, errs.CodeInternal, errs.ErrInternal.Msg)
			}
		case c.Writer.Written():
			slog.Error("handler 已写响应仍返回错误", "err", err, "method", c.Request.Method, "path", c.FullPath())
			c.Abort()
		default:
			FailErr(c, err)
		}
	}
}
