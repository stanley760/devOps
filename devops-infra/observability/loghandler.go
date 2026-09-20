package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceHandler 包装 slog.Handler，为每条日志自动附加当前 span 的
// trace_id / span_id（ctx 无有效 span 时原样输出）。
//
// 前置条件：调用方（中间件/handler/service）以 c.Request.Context() 或
// 请求派生的 ctx 作为 slog 第一个参数——本项目既有代码已满足。
// 日志采集后可按 trace_id 与 Jaeger/Tempo 互查。
type TraceHandler struct {
	Handler slog.Handler
}

// Enabled 实现 slog.Handler。
func (h TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

// Handle 实现 slog.Handler。
func (h TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs 实现 slog.Handler。
func (h TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return TraceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup 实现 slog.Handler。
func (h TraceHandler) WithGroup(name string) slog.Handler {
	return TraceHandler{Handler: h.Handler.WithGroup(name)}
}

var _ slog.Handler = TraceHandler{}
