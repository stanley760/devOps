package interceptor

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// AccessLog 记录每个 gRPC 请求完成后的访问信息（Debug 级）。
func AccessLog() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (any, error) {
		startedAt := time.Now()
		resp, err := handler(ctx, req)

		slog.Debug("gRPC 请求",
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"latency", time.Since(startedAt),
		)
		return resp, err
	}
}
