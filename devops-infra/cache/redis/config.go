package redis

import (
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/stanley760/devOps/devops-infra/config"
)

// Config Redis 连接配置。类型别名保持服务配置统一加载。
type Config = config.RedisConfig

// normalizeConfig 校验配置并填充默认值。
func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port <= 0 {
		return cfg, fmt.Errorf("redis.port 必须大于 0")
	}
	if cfg.DB < 0 {
		return cfg, fmt.Errorf("redis.db 不能为负数")
	}
	if cfg.PoolSize < 0 {
		return cfg, fmt.Errorf("redis.pool_size 不能为负数")
	}
	return cfg, nil
}

// options 生成 go-redis 配置。PoolSize 为 0 时交给 go-redis 使用内置默认值。
func options(cfg Config) *goredis.Options {
	return &goredis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	}
}
