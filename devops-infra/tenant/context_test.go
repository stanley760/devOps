package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestTenantContext(t *testing.T) {
	ctx := With(context.Background(), 42, "acme")
	if got, ok := TenantID(ctx); !ok || got != 42 {
		t.Fatalf("TenantID() = %d, %t; want 42, true", got, ok)
	}
	if got, err := RequiredTenantID(ctx); err != nil || got != 42 {
		t.Fatalf("RequiredTenantID() = %d, %v; want 42, nil", got, err)
	}
	if got, err := RequiredTenantCode(ctx); err != nil || got != "acme" {
		t.Fatalf("RequiredTenantCode() = %q, %v; want acme, nil", got, err)
	}
}

func TestTenantContextRejectsInvalidID(t *testing.T) {
	if _, err := WithTenant(context.Background(), 0); !errors.Is(err, ErrInvalidTenantID) {
		t.Fatalf("WithTenant() error = %v, want ErrInvalidTenantID", err)
	}
	if _, err := RequiredTenantID(context.Background()); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("RequiredTenantID() error = %v, want ErrTenantNotFound", err)
	}
}

func TestExecuteIgnoreRestoresIsolation(t *testing.T) {
	ctx, err := WithTenant(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	err = ExecuteIgnore(ctx, func(ignored context.Context) error {
		if !IsIgnore(ignored) {
			t.Fatal("ignore context is not marked")
		}
		if got, ok := TenantID(ignored); !ok || got != 42 {
			t.Fatalf("ignored context TenantID() = %d, %t; want 42, true", got, ok)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if IsIgnore(ctx) {
		t.Fatal("original context was modified")
	}
}
