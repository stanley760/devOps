package redis

import (
	"context"
	"fmt"
)

// Ping 检查 Redis 连接可用性，超时由调用方通过 context 控制。
func (client *Client) Ping(ctx context.Context) error {
	if client == nil || client.client == nil {
		return fmt.Errorf("Redis 客户端未初始化")
	}
	if err := client.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis 健康检查失败: %w", err)
	}
	return nil
}
