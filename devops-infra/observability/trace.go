package observability

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// TraceIDFromContext 返回 ctx 中当前 span 的 trace ID 字符串。
//
// 审计记录、日志关联等旁路设施据此与链路系统互查。
// 无有效 span（未装配 trace、ctx 为 nil、或 span 无效）时返回空串，
// 调用方应把空串视为"无链路信息"而非错误。
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanIDFromContext 返回 ctx 中当前 span 的 span ID 字符串；无有效 span 时返回空串。
func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasSpanID() {
		return ""
	}
	return sc.SpanID().String()
}

// HasTrace 判断 ctx 中是否存在有效 trace。
func HasTrace(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return trace.SpanContextFromContext(ctx).IsValid()
}