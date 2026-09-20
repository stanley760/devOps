package observability

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Middleware 返回 gin 入口 trace 埋点：
// 提取上游（Higress/前端）W3C traceparent → 建 server span → 结束记录路由属性。
// 挂载在业务中间件链最前，JWT/Tenant/Perm 与 handler 的耗时都落在同一 server span 内。
func Middleware(serviceName string, opts ...otelgin.Option) gin.HandlerFunc {
	return otelgin.Middleware(serviceName, opts...)
}

var (
	meter            = otel.Meter("devops-infra/observability")
	httpRequests     metric.Int64Counter
	httpDuration     metric.Float64Histogram
	httpInFlight     metric.Int64UpDownCounter
	metricsInitOnce  bool
	metricsInitError error
)

// RED 指标口径（经 Prometheus exporter 输出）：
//
//	http_server_requests_total{method,route,status}     计数
//	http_server_request_duration_seconds{method,route}  耗时直方图
//	http_server_in_flight                                在途请求数
//
// 标签基数控制：route 取 gin 路由模板（/users/:id），原始 path 不进标签；
// status 为有界 HTTP 状态码。禁止把 uid/tid 等高基数值放进指标标签。
func initMetrics() {
	if metricsInitOnce {
		return
	}
	metricsInitOnce = true
	httpRequests, metricsInitError = meter.Int64Counter("http_server_requests",
		metric.WithDescription("HTTP 请求总数"))
	if metricsInitError != nil {
		return
	}
	httpDuration, metricsInitError = meter.Float64Histogram("http_server_request_duration",
		metric.WithDescription("HTTP 请求耗时"),
		metric.WithUnit("s"))
	if metricsInitError != nil {
		return
	}
	httpInFlight, metricsInitError = meter.Int64UpDownCounter("http_server_in_flight",
		metric.WithDescription("进行中请求数"))
}

// MetricsMiddleware RED 埋点：挂在 trace 中间件之后。
// metrics 装配失败（MeterProvider 异常）时静默降级为直通，不影响业务请求。
func MetricsMiddleware() gin.HandlerFunc {
	initMetrics()
	broken := metricsInitError != nil

	return func(c *gin.Context) {
		if broken || httpRequests == nil {
			c.Next()
			return
		}

		httpInFlight.Add(c.Request.Context(), 1)
		startedAt := time.Now()
		c.Next()
		httpInFlight.Add(c.Request.Context(), -1)

		attrs := metric.WithAttributes(
			attribute.String("method", c.Request.Method),
			attribute.String("route", routeTemplate(c)),
			attribute.Int("status", c.Writer.Status()),
		)
		httpRequests.Add(c.Request.Context(), 1, attrs)
		httpDuration.Record(c.Request.Context(), time.Since(startedAt).Seconds(), attrs)
	}
}

// routeTemplate 返回路由模板；未匹配路由（404）归为 "unmatched"，防止高基数。
func routeTemplate(c *gin.Context) string {
	if path := c.FullPath(); path != "" {
		return path
	}
	return "unmatched"
}
