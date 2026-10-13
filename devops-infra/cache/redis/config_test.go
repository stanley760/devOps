package redis

import (
	"testing"

	"github.com/stanley760/devOps/devops-infra/config"
)

func TestNormalizeConfigDefaultsHost(t *testing.T) {
	cfg, err := normalizeConfig(config.RedisConfig{Port: 6379})
	if err != nil {
		t.Fatalf("normalizeConfig() error = %v", err)
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want 127.0.0.1", cfg.Host)
	}
}

func TestNormalizeConfigRejectsInvalidPort(t *testing.T) {
	if _, err := normalizeConfig(config.RedisConfig{}); err == nil {
		t.Fatal("port 未配置时应返回错误")
	}
}

func TestOptions(t *testing.T) {
	cfg := config.RedisConfig{
		Host:     "redis.local",
		Port:     6380,
		Password: "secret",
		DB:       2,
		PoolSize: 20,
	}

	got := options(cfg)
	if got.Addr != "redis.local:6380" || got.Password != "secret" || got.DB != 2 || got.PoolSize != 20 {
		t.Fatalf("options() = %+v", got)
	}
}
