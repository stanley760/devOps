package mysql

import (
	"fmt"
	"github.com/stanley760/devops/devops-infra/config"
	"gorm.io/gorm/logger"
	"time"
)

// Config MySQL 连接配置。这里保留类型别名，
// 使服务配置继续由 devops-infra/config 统一加载。
type Config = config.MysqlConfig

// normalize 填充默认值并校验必填项，避免把连接错误延迟到拨号阶段。
func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Host == "" {
		return cfg, fmt.Errorf("mysql.host 不能为空")
	}
	if cfg.Port <= 0 {
		return cfg, fmt.Errorf("mysql.port 必须大于 0")
	}
	if cfg.Username == "" {
		return cfg, fmt.Errorf("mysql.username 不能为空")
	}
	if cfg.Database == "" {
		return cfg, fmt.Errorf("mysql.database 不能为空")
	}
	if cfg.Charset == "" {
		cfg.Charset = "utf8mb4"
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 2
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = time.Hour
	}
	if cfg.ConnMaxIdleTime == 0 {
		cfg.ConnMaxIdleTime = time.Minute
	}
	return cfg, nil
}

// dsn 生成 Go MySQL Driver 的 DSN。
func configDSN(cfg Config) string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.Charset,
	)
}

// gormLogLevel 解析 GORM 日志级别，空值默认 warn。
func configGormLogLevel(cfg Config) logger.LogLevel {
	switch cfg.LogLevel {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}
