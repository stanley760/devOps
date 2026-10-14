package mysql

import (
	"github.com/stanley760/devops/devops-infra/config"
	"gorm.io/gorm/logger"
	"testing"
	"time"
)

func TestNormalizeConfig(t *testing.T) {
	cfg, err := normalizeConfig(Config{
		Host:     "127.0.0.1",
		Port:     3306,
		Username: "devops",
		Database: "devops",
	})
	if err != nil {
		t.Fatalf("normalizeConfig() error = %v", err)
	}
	if cfg.Charset != "utf8mb4" || cfg.MaxIdleConns != 2 || cfg.ConnMaxLifetime != time.Hour {
		t.Fatalf("默认值未生效: %+v", cfg)
	}
}

func TestNormalizeConfigRequiresHost(t *testing.T) {
	if _, err := normalizeConfig(config.MysqlConfig{}); err == nil {
		t.Fatal("缺少 host 时应返回错误")
	}
}

func TestConfigDSN(t *testing.T) {
	cfg := config.MysqlConfig{
		Host:     "db.local",
		Port:     3307,
		Username: "root",
		Password: "secret",
		Database: "devops",
		Charset:  "utf8mb4",
	}
	want := "root:secret@tcp(db.local:3307)/devops?charset=utf8mb4&parseTime=True&loc=Local"
	if got := configDSN(cfg); got != want {
		t.Fatalf("configDSN() = %q, want %q", got, want)
	}
}

func TestConfigGormLogLevel(t *testing.T) {
	cases := map[string]logger.LogLevel{
		"":       logger.Warn,
		"silent": logger.Silent,
		"error":  logger.Error,
		"warn":   logger.Warn,
		"info":   logger.Info,
		"other":  logger.Warn,
	}
	for input, want := range cases {
		if got := configGormLogLevel(config.MysqlConfig{LogLevel: input}); got != want {
			t.Fatalf("configGormLogLevel(%q) = %v, want %v", input, got, want)
		}
	}
}
