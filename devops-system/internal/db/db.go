// Package db 提供 devops-system 的 MySQL 连接管理。
//
// 连接为共享单例：dao 层统一入口见 tenantdb.DB(ctx)（多租户包装，P5 装配）；
// 种子/migrate 等无租户上下文场景直接使用 Get()。
package db

import (
	"fmt"
	"log/slog"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"devops-system/internal/config"
)

var current *gorm.DB

// Init 建立数据库连接并设置连接池参数。
func Init(cfg config.MysqlConfig) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.Charset,
	)

	gdb, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(parseLogLevel(cfg.LogLevel)),
	})
	if err != nil {
		return fmt.Errorf("连接 MySQL 失败: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return fmt.Errorf("获取底层连接池失败: %w", err)
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	current = gdb
	slog.Info("MySQL 连接成功",
		"addr", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), "database", cfg.Database)
	return nil
}

// Get 返回当前连接（未 Init 时为 nil）。
func Get() *gorm.DB { return current }

// Close 关闭连接池。
func Close() error {
	if current == nil {
		return nil
	}
	sqlDB, err := current.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// parseLogLevel GORM 日志级别。
func parseLogLevel(s string) gormlogger.LogLevel {
	switch s {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "info":
		return gormlogger.Info
	default: // warn（默认）
		return gormlogger.Warn
	}
}
