package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iddomain "github.com/stanley760/devOps/devops-id/internal/domain/idgen"
)

func TestMapErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"context canceled", context.Canceled, codes.Canceled},
		{"context deadline", context.DeadlineExceeded, codes.DeadlineExceeded},
		{"invalid name", iddomain.ErrInvalidName, codes.InvalidArgument},
		{"batch too large", iddomain.ErrBatchTooLarge, codes.InvalidArgument},
		{"clock backward", iddomain.ErrClockBackward, codes.Unavailable},
		{"lease lost", iddomain.ErrLeaseLost, codes.Unavailable},
		{"manager closed", iddomain.ErrManagerClosed, codes.Unavailable},
		{"slot occupied", iddomain.ErrSlotOccupied, codes.Unavailable},
		{"infra unavailable", iddomain.ErrInfraUnavailable, codes.Unavailable},
		{"no cached slot", iddomain.ErrNoCachedSlot, codes.Unavailable},
		{"slots exhausted", iddomain.ErrSlotsExhausted, codes.ResourceExhausted},
		{"too many tags", iddomain.ErrTooManyTags, codes.ResourceExhausted},
		{"slot mapping invalid", iddomain.ErrSlotMappingInvalid, codes.FailedPrecondition},
		{"unknown error", errors.New("boom"), codes.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := status.Convert(mapErr(tt.err))
			if st.Code() != tt.want {
				t.Fatalf("mapErr(%v) code = %v, want %v", tt.err, st.Code(), tt.want)
			}
		})
	}
}

func TestMapErrWrapped(t *testing.T) {
	wrapped := errors.New("wrapper: " + iddomain.ErrClockBackward.Error())
	wrappedErr := errors.Join(iddomain.ErrClockBackward, wrapped)
	st := status.Convert(mapErr(wrappedErr))
	if st.Code() != codes.Unavailable {
		t.Fatalf("wrapped error code = %v, want Unavailable", st.Code())
	}
}
