package observability

import (
	"errors"
	"fmt"

	redisotel "github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
	"gorm.io/gorm"
	otelform "gorm.io/plugin/opentelemetry/tracing"
)

// InstrumentGORM 为 GORM 安装 OTel 插件，生成数据库 client span。
// database 写入 db.name 属性；查询变量不进入 span，避免业务数据泄露。
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
