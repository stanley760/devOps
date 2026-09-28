package observability

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus/collectors"
	redisotel "github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
	"gorm.io/gorm"
	otelform "gorm.io/plugin/opentelemetry/tracing"
)

// InstrumentGORM 为 GORM 安装 OTel 插件，生成数据库 client span；
// 指标启用时同步注册 database/sql 连接池采集器（USE 视角，go_sql_* 系列指标）。
// database 写入 db.name 属性与 db_name 标签；查询变量不进入 span，避免业务数据泄露。
func (rt *Runtime) InstrumentGORM(db *gorm.DB, database string) error {
	if db == nil {
		return errors.New("observability: GORM DB 未初始化")
	}

	pluginOptions := []otelform.Option{
		otelform.WithDBSystem("mysql"),
		otelform.WithAttributes(semconv.DBNamespaceKey.String(database)),
		otelform.WithoutQueryVariables(),
	}
	if rt != nil && rt.tracerProvider != nil {
		pluginOptions = append(pluginOptions, otelform.WithTracerProvider(rt.tracerProvider))
	}
	if err := db.Use(otelform.NewPlugin(pluginOptions...)); err != nil {
		return fmt.Errorf("observability: 安装 GORM OTel 插件失败: %w", err)
	}

	// 连接池指标（USE 视角）：go_sql_open_connections / in_use / idle /
	// wait_count_total / wait_duration_seconds_total，db_name 为标签。
	// 指标未启用（registry 为 nil）时跳过，与 trace 侧 fail-open 一致；
	// 获取 *sql.DB 失败或重复注册仅告警，不阻断业务。
	if rt == nil || rt.registry == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		slog.Warn("observability: 获取底层 *sql.DB 失败，连接池指标未装配", "err", err)
		return nil
	}
	if err := rt.registry.Register(collectors.NewDBStatsCollector(sqlDB, database)); err != nil {
		slog.Warn("observability: 注册 SQL 连接池指标失败", "db_name", database, "err", err)
	}
	return nil
}

// InstrumentRedis 为 go-redis 安装 trace 与连接池指标钩子。
// 原始 Redis 命令不进入 span，避免 key/value 中的敏感信息泄露。
func (rt *Runtime) InstrumentRedis(client goredis.UniversalClient) error {
	if client == nil {
		return errors.New("observability: Redis 客户端未初始化")
	}

	tracingOptions := []redisotel.TracingOption{
		redisotel.WithDBStatement(false),
		redisotel.WithCallerEnabled(false),
	}
	if rt != nil && rt.tracerProvider != nil {
		tracingOptions = append(tracingOptions, redisotel.WithTracerProvider(rt.tracerProvider))
	}
	if err := redisotel.InstrumentTracing(client, tracingOptions...); err != nil {
		return fmt.Errorf("observability: 安装 Redis OTel tracing 失败: %w", err)
	}

	metricsOptions := []redisotel.MetricsOption{}
	if rt != nil && rt.meterProvider != nil {
		metricsOptions = append(metricsOptions, redisotel.WithMeterProvider(rt.meterProvider))
	}
	if err := redisotel.InstrumentMetrics(client, metricsOptions...); err != nil {
		return fmt.Errorf("observability: 安装 Redis OTel metrics 失败: %w", err)
	}
	return nil
}
