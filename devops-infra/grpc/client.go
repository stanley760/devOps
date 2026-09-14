package grpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Dial 拨号 gRPC 服务，返回的就绪连接（非阻塞，可重连）。
// 采用内置 Unavailable 指数退避重试策略：发号类调用幂等安全（重试至多烧号，绝不重号）。
func Dial(ctx context.Context, endpoint string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("拨号 gRPC %s 失败: %w", endpoint, err)
	}
	return conn, nil
}

// IsUnavailable 判断错误是否为 Unavailable（连接断开/服务不可达），
// 供调用方决定是否重试。
func IsUnavailable(err error) bool {
	return status.Code(err) == codes.Unavailable
}

// Close 关闭客户端连接。
func Close(conn *grpc.ClientConn) error {
	if conn == nil {
		return nil
	}
	if err := conn.Close(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("关闭 gRPC 连接失败: %w", err)
	}
	return nil
}
