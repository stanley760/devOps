// Package observability 统一装配 OpenTelemetry 可观测性：
// trace（OTLP 出口）+ metrics（Prometheus exporter）+ 日志 trace 关联。
//
// 装配位置：各服务在 logging.Init 之后、任何请求处理之前调用 Init，
// 进程退出前调用 Runtime.Shutdown flush 在途数据（对齐 logging.Close 的收尾语义）。
//
// 埋点一次、两处消费：业务代码只面对 OTel API；
// trace 经 OTLP HTTP 发往 Jaeger/Tempo，metrics 经 Prometheus exporter
// 由 Prometheus 抓取 /metrics，无需双重埋点。
package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config 可观测性装配配置。
type Config struct {
	ServiceName    string  // 服务名；应与 nacos 注册名一致（跨服务 trace 与 Grafana 过滤依赖）
	Env            string  // deployment.environment 资源属性（dev/test/prod）
	OTLPEndpoint   string  // OTLP HTTP 端点，如 "http://jaeger:4318"；空 = 不装配 trace 出口
	SamplerRatio   float64 // 根 span 采样率；dev 建议 1.0，prod 建议 0.1；<=0 视为 1.0
	MetricsEnabled bool    // 是否装配 Prometheus exporter（/metrics）
}

// Runtime 装配产物：MetricsHandler 非 nil 时挂载到公开路由 /metrics。
type Runtime struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	MetricsHandler http.Handler
}

// Init 装配全局 OTel（TracerProvider / MeterProvider / W3C 传播器）并做日志 trace 关联。
//
// fail-open 设计：可观测性故障不应阻断业务启动——OTLP exporter 创建失败仅记日志并
// 跳过 trace 出口（本地 span 仍创建，只是不上报）；Resource 构建失败才视为装配错误。
func Init(ctx context.Context, cfg Config) (*Runtime, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcessRuntimeVersion(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.DeploymentEnvironment(envOrUnknown(cfg.Env)),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("observability: 构建资源失败: %w", err)
	}

	rt := &Runtime{}
	traceOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}

	if cfg.OTLPEndpoint != "" {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint))
		if err != nil {
			// exporter 不可用不阻断启动：记日志、跳过出口（本地 span 照常）
			slog.Warn("observability: 创建 OTLP exporter 失败，trace 出口关闭", "err", err, "endpoint", cfg.OTLPEndpoint)
		} else {
			traceOpts = append(traceOpts,
				sdktrace.WithBatcher(exp,
					sdktrace.WithMaxQueueSize(8192),
					sdktrace.WithBatchTimeout(5*time.Second),
				),
				sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplerRatio(cfg.SamplerRatio)))),
			)
		}
	}
	rt.tracerProvider = sdktrace.NewTracerProvider(traceOpts...)

	if cfg.MetricsEnabled {
		// 自定义 registry：OTel 指标 + Go 运行时/进程指标（GC、goroutine、内存）
		// 同处一个 /metrics 端点，Grafana 面板可直接引用 runtime 曲线
		registry := prometheus.NewRegistry()
		registry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
		exp, err := promexporter.New(promexporter.WithRegisterer(registry))
		if err != nil {
			return nil, fmt.Errorf("observability: 创建 Prometheus exporter 失败: %w", err)
		}
		rt.meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(exp),
		)
		rt.MetricsHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	}

	otel.SetTracerProvider(rt.tracerProvider)
	if rt.meterProvider != nil {
		otel.SetMeterProvider(rt.meterProvider)
	}
	// W3C tracecontext + baggage：与 Higress 网关、前端及跨服务 gRPC/HTTP 传播对齐
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// 日志 trace 关联：包装当前默认 handler，现有 slog 调用自动携带 trace_id/span_id
	slog.SetDefault(slog.New(TraceHandler{Handler: slog.Default().Handler()}))

	return rt, nil
}

// Shutdown flush 在途 trace/metrics 后释放；进程退出前调用（带超时 ctx）。
func (rt *Runtime) Shutdown(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	var errs []error
	if rt.meterProvider != nil {
		if err := rt.meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider: %w", err))
		}
	}
	if rt.tracerProvider != nil {
		if err := rt.tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider: %w", err))
		}
	}
	return errors.Join(errs...)
}

func envOrUnknown(env string) string {
	if env == "" {
		return "unknown"
	}
	return env
}

func samplerRatio(ratio float64) float64 {
	if ratio <= 0 {
		return 1.0
	}
	if ratio > 1 {
		return 1.0
	}
	return ratio
}
