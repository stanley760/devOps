package observability

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

func TestInstrumentGORMCreatesSpanAndRedactsVariables(t *testing.T) {
	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExporter))
	runtime := &Runtime{tracerProvider: tracerProvider}

	db, err := gorm.Open(sqlite.Open("file:instrumentation_gorm?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := runtime.InstrumentGORM(db, "devops_system"); err != nil {
		t.Fatalf("InstrumentGORM() error = %v", err)
	}

	var value int
	if err := db.Raw("SELECT ? AS value", 42).Scan(&value).Error; err != nil {
		t.Fatalf("query sqlite: %v", err)
	}
	if value != 42 {
		t.Fatalf("query result = %d, want 42", value)
	}

	var querySpan *tracetest.SpanStub
	for i, span := range spanExporter.GetSpans() {
		if span.SpanKind == trace.SpanKindClient {
			querySpan = &spanExporter.GetSpans()[i]
			break
		}
	}
	if querySpan == nil {
		t.Fatalf("GORM client span not recorded: %+v", spanExporter.GetSpans())
	}

	attrs := attributesMap(querySpan.Attributes)
	if attrs["db.namespace"] != "devops_system" {
		t.Fatalf("db.namespace attribute = %q, want devops_system", attrs["db.namespace"])
	}
	if query, ok := attrs["db.query.text"]; ok && strings.Contains(query, "42") {
		t.Fatalf("query variable leaked into span: %q", query)
	}
}

func TestInstrumentRedisCreatesSpanAndMetrics(t *testing.T) {
	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer redisServer.Close()

	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExporter))
	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	runtime := &Runtime{tracerProvider: tracerProvider, meterProvider: meterProvider}

	redisClient := goredis.NewClient(&goredis.Options{Addr: redisServer.Addr()})
	defer func() { _ = redisClient.Close() }()
	if err := runtime.InstrumentRedis(redisClient); err != nil {
		t.Fatalf("InstrumentRedis() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := redisClient.Set(ctx, "sensitive-key", "sensitive-value", time.Minute).Err(); err != nil {
		t.Fatalf("redis set: %v", err)
	}

	foundSpan := false
	for _, span := range spanExporter.GetSpans() {
		if span.SpanKind != trace.SpanKindClient {
			continue
		}
		foundSpan = true
		for _, attr := range span.Attributes {
			if strings.Contains(attr.Value.Emit(), "sensitive-") {
				t.Fatalf("sensitive redis command data leaked into span: %+v", span.Attributes)
			}
		}
	}
	if !foundSpan {
		t.Fatalf("Redis client span not recorded: %+v", spanExporter.GetSpans())
	}

	var metrics metricdata.ResourceMetrics
	if err := metricReader.Collect(ctx, &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if !hasMetric(metrics, "db.client.connections.usage") {
		t.Fatalf("Redis connection metric not recorded: %+v", metrics)
	}
}

func attributesMap(attrs []attribute.KeyValue) map[string]string {
	result := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		result[string(attr.Key)] = attr.Value.Emit()
	}
	return result
}

func hasMetric(metrics metricdata.ResourceMetrics, name string) bool {
	for _, scopeMetrics := range metrics.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name == name {
				return true
			}
		}
	}
	return false
}
