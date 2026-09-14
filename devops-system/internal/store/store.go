// Package store 提供 devops-system 的缓存装配入口。
//
// 新代码优先使用 Cache() 返回的 cache.Cache 接口；
// 会话 pipeline 等框架高级能力可通过 Get() 获取 go-redis 客户端。
package store

import (
	"context"

	goredis "github.com/redis/go-redis/v9"

	"devops-infra/cache"
	rediscache "devops-infra/cache/redis"
	"devops-infra/config"
)

// Init 建立 Redis 连接。
func Init(cfg config.RedisConfig) error {
	return rediscache.Init(cfg)
}

// Cache 返回通用缓存接口；未初始化时为 nil。
func Cache() cache.Cache {
	if rediscache.Default() == nil {
		return nil
	}
	return rediscache.Default()
}

// Get 返回当前 go-redis 客户端；未 Init 时为 nil。
func Get() *goredis.Client {
	return rediscache.Get()
}

// Close 关闭连接池。
func Close() error {
	return rediscache.Close()
}

// Ping 检查当前 Redis 连接。
func Ping(ctx context.Context) error {
	return rediscache.Default().Ping(ctx)
}
