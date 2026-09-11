package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog 记录每个 HTTP 请求完成后的访问信息。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		slog.Debug("HTTP 请求",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency", time.Since(startedAt),
			"bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
		)
	}
}
