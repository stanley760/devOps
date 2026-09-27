// OTLP 出口回归：修复前 WithEndpointURL("http://host:4318") 无 path 会规范化为 "/"，
// trace 发到 / 被 Jaeger 404 静默丢弃；修复后应自动补全 /v1/traces。
package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestOTLPExportPathCollected 无 path 端点 → 请求必须发往 /v1/traces（非 /）。
func TestOTLPExportPathCollected(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	var requests int

	// 模拟 OTLP 接收端（Jaeger 行为：/v1/traces 才收数据）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		gotPath = r.URL.Path
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt, err := Init(context.Background(), Config{
		ServiceName:  "otlp-path-test",
		Env:          "dev",
		OTLPEndpoint: srv.URL, // http://127.0.0.1:PORT 无 path —— 复现业务配置形态
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	defer func() { _ = rt.Shutdown(context.Background()) }()

	// 产生一个 span 并同步 flush
	tracer := rt.tracerProvider.Tracer("test")
	_, span := tracer.Start(context.Background(), "smoke-span")
	span.End()

	flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.tracerProvider.ForceFlush(flushCtx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests == 0 {
		t.Fatal("OTLP 接收端未收到任何请求——trace 被静默丢弃（修复前行为：发往 /）")
	}
	if gotPath != "/v1/traces" {
		t.Fatalf("OTLP 请求路径 = %q, want /v1/traces（无 path 端点必须补全默认信号路径）", gotPath)
	}
}
