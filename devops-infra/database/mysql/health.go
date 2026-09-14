package mysql

import (
	"context"
	"fmt"
)

// Ping 检查底层连接池可用性，超时由调用方通过 context 控制。
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("MySQL 客户端未初始化")
	}
	pool, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("获取底层连接池失败: %w", err)
	}
	if err := pool.PingContext(ctx); err != nil {
		return fmt.Errorf("MySQL 健康检查失败: %w", err)
	}
	return nil
}
