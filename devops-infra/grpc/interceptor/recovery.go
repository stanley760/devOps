// Package interceptor 提供 gRPC 服务端通用拦截器。
package interceptor

import (
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Recovery 把 handler 的 panic 转成 Internal 错误，
// 堆栈进日志，不向客户端泄露细节（对齐 HTTP 侧 Recovery 中间件语义）。
func Recovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("gRPC panic", "method", info.FullMethod, "panic", recovered,
					"stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "服务器内部错误")
			}
		}()
		return handler(ctx, req)
	}
}
