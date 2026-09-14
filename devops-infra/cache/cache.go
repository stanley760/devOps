// Package cache 定义通用缓存接口与错误语义。
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound 表示缓存 key 不存在或已过期。
var ErrNotFound = errors.New("cache: key not found")

// Cache 是业务层需要的最小缓存接口。
// expiration <= 0 表示不过期。
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, expiration time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// Health 支持健康检查的缓存实现。
type Health interface {
	Ping(ctx context.Context) error
}

// Closer 支持释放连接资源的缓存实现。
type Closer interface {
	Close() error
}
