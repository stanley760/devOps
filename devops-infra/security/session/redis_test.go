package session

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func testStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisClientStore(client, time.Hour), mr
}

func TestRedisStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	store, _ := testStore(t)
	old := Session{TenantID: 7, UserID: 11, SessionID: "old"}
	if err := store.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	active, err := store.Active(ctx, SessionRef{TenantID: 7, UserID: 11, SessionID: "old"})
	if err != nil || !active {
		t.Fatalf("active = %v, %v", active, err)
	}
	tid, uid, ok := store.Lookup(ctx, "old")
	if !ok || tid != 7 || uid != 11 {
		t.Fatalf("lookup = %d, %d, %v", tid, uid, ok)
	}
	matched, err := store.Matches(ctx, SessionRef{TenantID: 7, UserID: 11, SessionID: "old"}, 11)
	if err != nil || !matched {
		t.Fatalf("match = %v, %v", matched, err)
	}
	if err := store.Create(ctx, Session{TenantID: 7, UserID: 11, SessionID: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, SessionRef{TenantID: 7, UserID: 11, SessionID: "old"}); err != nil {
		t.Fatal(err)
	}
	active, err = store.Active(ctx, SessionRef{TenantID: 7, UserID: 11, SessionID: "old"})
	if err != nil || active {
		t.Fatalf("old active = %v, %v", active, err)
	}
	if _, _, ok := store.Lookup(ctx, "old"); ok {
		t.Fatal("old lookup unexpectedly succeeded")
	}
	if err := store.RevokeUser(ctx, 7, 11); err != nil {
		t.Fatal(err)
	}
	active, err = store.Active(ctx, SessionRef{TenantID: 7, UserID: 11, SessionID: "new"})
	if err != nil || active {
		t.Fatalf("new active after user revoke = %v, %v", active, err)
	}
}

func TestRedisStoreRevokeTenantAndMalformedLookup(t *testing.T) {
	ctx := context.Background()
	store, mr := testStore(t)
	for _, sid := range []string{"a", "b"} {
		if err := store.Create(ctx, Session{TenantID: 9, UserID: 3, SessionID: sid}); err != nil {
			t.Fatal(err)
		}
	}
	mr.Set("auth:sid2uid:bad", "not-a-pair")
	if _, _, ok := store.Lookup(ctx, "bad"); ok {
		t.Fatal("malformed lookup unexpectedly succeeded")
	}
	if err := store.RevokeTenant(ctx, 9); err != nil {
		t.Fatal(err)
	}
	active, err := store.Active(ctx, SessionRef{TenantID: 9, UserID: 3, SessionID: "a"})
	if err != nil || active {
		t.Fatalf("tenant revoke active = %v, %v", active, err)
	}
}

func TestRedisStoreValidation(t *testing.T) {
	store, _ := testStore(t)
	ctx := context.Background()
	if err := store.Create(ctx, Session{TenantID: 0, UserID: 1, SessionID: "x"}); err != ErrInvalidTenantID {
		t.Fatalf("create invalid tenant = %v", err)
	}
	if _, err := store.Active(ctx, SessionRef{TenantID: 1, UserID: 1}); err != ErrInvalidSession {
		t.Fatalf("active invalid sid = %v", err)
	}
}
