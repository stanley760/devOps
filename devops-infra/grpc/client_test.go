package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestDialWithOptionsCreatesClientSpan(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, health.NewServer())
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExporter))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	defer otel.SetTracerProvider(previousProvider)
	defer func() { _ = tracerProvider.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := DialWithOptions(ctx, listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("DialWithOptions() error = %v", err)
	}
	defer func() { _ = Close(conn) }()

	if _, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("health check: %v", err)
	}

	for _, span := range spanExporter.GetSpans() {
		if span.Name == "grpc.health.v1.Health/Check" && span.SpanKind == trace.SpanKindClient {
			return
		}
	}
	t.Fatalf("client span for health check not recorded: %+v", spanExporter.GetSpans())
}
