// Package mysql 提供 MySQL 数据库适配器：连接池、事务与健康检查。
//
// 推荐在新代码中使用 Client 并在 cmd 层显式注入；
// Init/Get/Close 是兼容现有调用方的过渡接口。
package mysql

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	database "github.com/stanley760/devops/devops-infra/database"
)

// Client 封装 GORM MySQL 连接池。
type Client struct {
	db *gorm.DB
}

// New 建立连接并应用连接池与 GORM 日志配置。
func New(cfg Config) (*Client, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(mysql.Open(configDSN(cfg)), &gorm.Config{
		Logger: gormlogger.Default.LogMode(configGormLogLevel(cfg)),
	})
	if err != nil {
		return nil, fmt.Errorf("连接 MySQL 失败: %w", err)
	}

	pool, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接池失败: %w", err)
	}
	pool.SetMaxIdleConns(cfg.MaxIdleConns)
	pool.SetMaxOpenConns(cfg.MaxOpenConns)
	pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	slog.Info("MySQL 连接成功",
		"addr", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		"database", cfg.Database)
	return &Client{db: db}, nil
}

// DB 返回底层 GORM 连接，供既有 Repository 或迁移工具使用。
func (c *Client) DB() *gorm.DB {
	if c == nil {
		return nil
	}
	return c.db
}

// Session 返回当前 context 中的事务；无事务时返回连接池会话。
func (c *Client) Session(ctx context.Context) *gorm.DB {
	if tx, ok := database.ExecutorFromContext(ctx); ok {
		return tx
	}
	return c.db.WithContext(ctx)
}

// Transaction 开启事务并把 tx 注入 fn 的 context，Repository 通过 Session 复用。
func (c *Client) Transaction(ctx context.Context, fn database.TxFunc) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("MySQL 客户端未初始化")
	}
	return database.Transaction(ctx, c.db.WithContext(ctx), fn)
}

// Close 关闭连接池。
func (c *Client) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	pool, err := c.db.DB()
	if err != nil {
		return err
	}
	return pool.Close()
}

var current *Client

// Init 建立全局连接，兼容现有命令与旧调用方。
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

// Get 返回当前底层 GORM 连接；未 Init 时为 nil。
func Get() *gorm.DB {
	return current.DB()
}

// Close 关闭全局连接池。
func Close() error {
	client := current
	current = nil
	return client.Close()
}
