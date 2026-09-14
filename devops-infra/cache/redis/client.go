// Package redis 基于 github.com/redis/go-redis/v9 提供缓存适配器。
package redis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"devops-infra/cache"
)

// Client 封装 go-redis 客户端并实现 cache.Cache。
var _ cache.Cache = (*Client)(nil)

type Client struct {
	client *goredis.Client
}

// New 创建客户端并验证连接。
func New(cfg Config) (*Client, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	client := goredis.NewClient(options(cfg))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}

	slog.Info("Redis 连接成功", "addr", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), "db", cfg.DB)
	return &Client{client: client}, nil
}

// Redis 返回底层 go-redis 客户端，供 pipeline、订阅等高级能力使用。
func (client *Client) Redis() *goredis.Client {
	if client == nil {
		return nil
	}
	return client.client
}

// Get 读取缓存；key 不存在时返回 cache.ErrNotFound。
func (client *Client) Get(ctx context.Context, key string) (string, error) {
	if client == nil || client.client == nil {
		return "", fmt.Errorf("Redis 客户端未初始化")
	}

	value, err := client.client.Get(ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		return "", cache.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("读取 Redis 缓存失败: %w", err)
	}
	return value, nil
}

// Set 写入缓存；expiration <= 0 表示不过期。
func (client *Client) Set(ctx context.Context, key, value string, expiration time.Duration) error {
	if client == nil || client.client == nil {
		return fmt.Errorf("Redis 客户端未初始化")
	}
	if err := client.client.Set(ctx, key, value, expiration).Err(); err != nil {
		return fmt.Errorf("写入 Redis 缓存失败: %w", err)
	}
	return nil
}

// Delete 删除一个或多个 key。
func (client *Client) Delete(ctx context.Context, keys ...string) error {
	if client == nil || client.client == nil {
		return fmt.Errorf("Redis 客户端未初始化")
	}
	if len(keys) == 0 {
		return nil
	}
	if err := client.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("删除 Redis 缓存失败: %w", err)
	}
	return nil
}

// Exists 判断 key 是否存在。
func (client *Client) Exists(ctx context.Context, key string) (bool, error) {
	if client == nil || client.client == nil {
		return false, fmt.Errorf("Redis 客户端未初始化")
	}
	count, err := client.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("检查 Redis 缓存失败: %w", err)
	}
	return count > 0, nil
}

// Close 关闭连接池。
func (client *Client) Close() error {
	if client == nil || client.client == nil {
		return nil
	}
	return client.client.Close()
}

var current *Client

// Init 建立全局 Redis 客户端，兼容既有装配代码。
func Init(cfg Config) error {
	client, err := New(cfg)
	if err != nil {
		return err
	}
	current = client
	return nil
}

// Default 返回当前全局客户端。
func Default() *Client { return current }

// Get 返回当前底层 go-redis 客户端；未 Init 时为 nil。
func Get() *goredis.Client {
	return current.Redis()
}

// Close 关闭全局客户端。
func Close() error {
	client := current
	current = nil
	return client.Close()
}
