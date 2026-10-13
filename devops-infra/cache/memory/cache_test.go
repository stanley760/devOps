package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	cacheinf "github.com/stanley760/devOps/devops-infra/cache"
)

func TestCacheLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	c := New()
	c.now = func() time.Time { return now }

	ctx := context.Background()
	if err := c.Set(ctx, "user", "alice", 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if value, err := c.Get(ctx, "user"); err != nil || value != "alice" {
		t.Fatalf("Get() = %q, %v; want alice, <nil>", value, err)
	}

	exists, err := c.Exists(ctx, "user")
	if err != nil || !exists {
		t.Fatalf("Exists() = %t, %v; want true, <nil>", exists, err)
	}

	if err := c.Delete(ctx, "user"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := c.Get(ctx, "user"); !errors.Is(err, cacheinf.ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, cacheinf.ErrNotFound)
	}
}

func TestCacheExpires(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	c := New()
	c.now = func() time.Time { return now }

	ctx := context.Background()
	if err := c.Set(ctx, "session", "sid", time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	now = now.Add(time.Minute)
	if _, err := c.Get(ctx, "session"); !errors.Is(err, cacheinf.ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, cacheinf.ErrNotFound)
	}

	exists, err := c.Exists(ctx, "session")
	if err != nil || exists {
		t.Fatalf("Exists() = %t, %v; want false, <nil>", exists, err)
	}
}
