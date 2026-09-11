// Package store 提供 devops-system 的 Redis 连接管理（会话/租户缓存）。
package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"devops-system/internal/config"
)

var current *redis.Client

// Init 建立 Redis 连接（惰性，真正连通性在 Ping 时验证）。
func Init(cfg config.RedisConfig) error {
	current = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := current.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("连接 Redis 失败: %w", err)
	}
	slog.Info("Redis 连接成功", "addr", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), "db", cfg.DB)
	return nil
}

// Get 返回当前客户端（未 Init 时为 nil）。
func Get() *redis.Client { return current }

// Close 关闭连接池。
func Close() error {
	if current == nil {
		return nil
	}
	return current.Close()
}
