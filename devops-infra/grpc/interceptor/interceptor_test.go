package interceptor

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRecoveryConvertsPanicToInternal(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/devops.test/Run"}
	resp, err := Recovery()(context.Background(), nil, info,
		func(context.Context, any) (any, error) {
			panic("boom")
		})
	if resp != nil {
		t.Fatalf("resp = %v, want nil", resp)
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want %v", status.Code(err), codes.Internal)
	}
}

func TestAccessLogPassesThrough(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/devops.test/Run"}
	want := errors.New("business error")

	resp, err := AccessLog()(context.Background(), nil, info,
		func(context.Context, any) (any, error) {
			return "ok", want
		})
	if resp != "ok" {
		t.Fatalf("resp = %v, want ok", resp)
	}
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
