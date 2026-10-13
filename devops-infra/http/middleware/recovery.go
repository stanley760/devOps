// Package middleware 存放 gin 中间件。
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	commonResp "github.com/stanley760/devOps/devops-common/response"
	"github.com/stanley760/devOps/devops-infra/errs"
)

// Recovery panic 统一处理：记录堆栈后返回 500 错误信封。
// 不用 gin.Recovery()，其默认输出纯文本，不符合统一响应格式。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic",
					"error", r,
					"method", c.Request.Method,
					"path", c.FullPath(),
					"stack", string(debug.Stack()),
				)
				if c.Writer.Written() {
					// 部分响应已写出，无法再改写为信封，仅置 500
					c.AbortWithStatus(http.StatusInternalServerError)
					return
				}
				c.AbortWithStatusJSON(http.StatusInternalServerError, commonResp.BaseResponse[any]{
					Code: errs.CodeInternal,
					Msg:  errs.ErrInternal.Msg,
				})
			}
		}()
		c.Next()
	}
}
