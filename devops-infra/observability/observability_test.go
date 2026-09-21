package observability

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInitDefaults(t *testing.T) {
	rt, err := Init(context.Background(), Config{
		ServiceName: "test-svc",
		Env:         "dev",
		// OTLPEndpoint 留空：不装配出口，本地 span 照常
		MetricsEnabled: true,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer func() { _ = rt.Shutdown(context.Background()) }()

	if rt.MetricsHandler == nil {
		t.Fatal("MetricsHandler should be non-nil when MetricsEnabled")
	}

	// /metrics 端点应输出 Go 运行时指标
	rec := httptest.NewRecorder()
	rt.MetricsHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Fatal("metrics should expose go runtime metrics")
	}

	// Shutdown 幂等安全
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestSamplerRatioClamping(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0, 1.0}, {-1, 1.0}, {0.1, 0.1}, {1.5, 1.0},
	}
	for _, c := range cases {
		if got := samplerRatio(c.in); got != c.want {
			t.Fatalf("samplerRatio(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTraceHandlerAttachesTraceIDs(t *testing.T) {
	var buf strings.Builder
	inner := slog.NewTextHandler(&buf, nil)
	handler := TraceHandler{Handler: inner}
	logger := slog.New(handler)

	// 无 span 上下文：不注入 trace_id
	logger.InfoContext(context.Background(), "no-span")
	if strings.Contains(buf.String(), "trace_id") {
		t.Fatalf("unexpected trace_id without span: %s", buf.String())
	}

	// 有 span 上下文：注入 trace_id/span_id
	tp := sdktrace.NewTracerProvider()
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(old)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	logger.InfoContext(ctx, "with-span")
	span.End()

	out := buf.String()
	if !strings.Contains(out, "trace_id="+span.SpanContext().TraceID().String()) {
		t.Fatalf("log missing trace_id: %s", out)
	}
	if !strings.Contains(out, "span_id="+span.SpanContext().SpanID().String()) {
		t.Fatalf("log missing span_id: %s", out)
	}
}

func TestMetricsMiddlewareRecordsAndPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rt, err := Init(context.Background(), Config{ServiceName: "t", Env: "dev", MetricsEnabled: true})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer func() { _ = rt.Shutdown(context.Background()) }()

	router := gin.New()
	router.Use(rt.MetricsMiddleware())
	router.GET("/users/:id", func(c *gin.Context) { c.Status(http.StatusTeapot) })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("handler passthrough broken: %d", rec.Code)
	}

	// 指标经 exporter 输出可被抓取（计数器存在且带路由模板标签）
	mrec := httptest.NewRecorder()
	rt.MetricsHandler.ServeHTTP(mrec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := mrec.Body.String()
	if !strings.Contains(body, "http_server_requests_total") ||
		!strings.Contains(body, `route="/users/:id"`) ||
		!strings.Contains(body, `status="418"`) {
		t.Fatalf("metric with route template label missing:\n%s", firstLines(body, 5))
	}
	// Resource 常量标签：service_name / deployment_environment 随每条指标输出，
	// Grafana 面板按 service_name 过滤依赖此标签
	if !strings.Contains(body, `service_name="t"`) || !strings.Contains(body, `deployment_environment="dev"`) {
		t.Fatalf("resource constant labels missing:\n%s", firstLines(body, 5))
	}

	// 未匹配路由归为 unmatched（防高基数）
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	mrec = httptest.NewRecorder()
	rt.MetricsHandler.ServeHTTP(mrec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(mrec.Body.String(), `route="unmatched"`) {
		t.Fatal("unmatched route should collapse to 'unmatched' label")
	}

	_ = time.Second
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
