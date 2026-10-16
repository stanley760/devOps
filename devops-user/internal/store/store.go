// Package store 提供 devops-user 的 Redis 装配入口。
//
// 与 devops-system 的差异：不提供 Cache()（C 端无租户缓存等通用缓存需求），
// 只保留 JWT 会话态所需的 go-redis 客户端。
//
// 装配直接委托 devops-infra/cache/redis，不重复实现连接逻辑。
package store

import (
	"context"

	goredis "github.com/redis/go-redis/v9"

	rediscache "github.com/stanley760/devops/devops-infra/cache/redis"
	"github.com/stanley760/devops/devops-infra/config"
)

// Init 建立 Redis 连接。
func Init(cfg config.RedisConfig) error {
	return rediscache.Init(cfg)
}

// Get 返回当前 go-redis 客户端；未 Init 时为 nil。
func Get() *goredis.Client {
	return rediscache.Get()
}

// Close 关闭连接池。
func Close() error {
	return rediscache.Close()
}

// Ping 检查当前 Redis 连接。签名对齐 httpinfra.ReadinessCheck（ctx 在此不参与，
// go-redis 的 Ping 自身使用内部重试与超时配置）。
func Ping(_ context.Context) error {
	return rediscache.Default().Ping(context.Background())
}
