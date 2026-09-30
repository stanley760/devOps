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
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
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
	// registry 是 /metrics 的自建注册表；指标未启用时为 nil。
	// 依赖 USE 指标（如 SQL 连接池）通过它注册，与 OTel 指标同端点输出。
	registry       *prometheus.Registry
	MetricsHandler http.Handler

	serviceName  string
	httpRequests metric.Int64Counter
	httpDuration metric.Float64Histogram
	httpInFlight metric.Int64UpDownCounter
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
	rt.serviceName = cfg.ServiceName
	traceOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}

	if cfg.OTLPEndpoint != "" {
		otlpOpts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint)}
		// WithEndpointURL 对无 path 的 URL 会规范化为 "/"，且不追加默认信号路径 /v1/traces
		// （otlptracehttp options.go:106-109）——发到 / 会被 Jaeger 404 拒收，trace 静默丢弃。
		// 仅当业务配置未显式带 path 时补全；Insecure 由 scheme 自动推导（http→明文）。
		if u, perr := url.Parse(cfg.OTLPEndpoint); perr == nil && u.Path == "" {
			otlpOpts = append(otlpOpts, otlptracehttp.WithURLPath("/v1/traces"))
		}
		exp, err := otlptracehttp.New(ctx, otlpOpts...)
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
		rt.registry = registry
		exp, err := promexporter.New(
			promexporter.WithRegisterer(registry),
			// 将 OTel Resource 的 service.name / deployment.environment 提升为常量标签，
			// Grafana 面板按 service_name 维度过滤/聚合；
			// 其余资源属性（进程/运行时版本等）不进标签，控制series基数。
			promexporter.WithResourceAsConstantLabels(func(kv attribute.KeyValue) bool {
				return kv.Key == semconv.ServiceNameKey || kv.Key == semconv.DeploymentEnvironmentKey
			}),
		)
		if err != nil {
			return nil, fmt.Errorf("observability: 创建 Prometheus exporter 失败: %w", err)
		}
		rt.meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(exp),
		)
		rt.MetricsHandler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
		if err := rt.initHTTPMetrics(); err != nil {
			_ = rt.meterProvider.Shutdown(ctx)
			return nil, fmt.Errorf("observability: 初始化 HTTP 指标失败: %w", err)
		}
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

	// 日志 trace 关联：包装当前默认 handler，现有 slog 调用自动携带 trace_id/span_id。
	// 自递归防护：SetDefault 会把 log 包默认输出桥接回 slog.Default()；若当前默认
	// handler 仍是内建 defaultHandler（写 log.Default()），TraceHandler→log.Default()
	// →TraceHandler 成环，请求路径第一条日志即在 log 互斥锁上自死锁。log 默认
	// writer 仍是 os.Stderr 说明从未 SetDefault 过（logging.Init 未先行，典型如
	// 单元测试），此时以等价的直写 stderr handler 顶替；生产路径 logging.Init 的
	// 文件/控制台 handler 直写 writer、不经 log.Default()，原样包装。
	base := slog.Default().Handler()
	if log.Default().Writer() == os.Stderr {
		base = slog.NewTextHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(TraceHandler{Handler: base}))

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

// ShutdownWithTimeout 使用有界超时 flush 在途数据，避免外部后端不可用时阻塞进程退出。
func (rt *Runtime) ShutdownWithTimeout(parent context.Context, timeout time.Duration) error {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return rt.Shutdown(ctx)
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
