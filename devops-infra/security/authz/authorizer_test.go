package authz

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	return db
}

func seedBasePolicies(t *testing.T, manager PolicyManager) {
	t.Helper()

	for _, seed := range []struct {
		ptype  string
		values []string
	}{
		{ptype: "p", values: []string{"platform_admin", "*", "*"}},
		{ptype: "g2", values: []string{"10", "platform_admin"}},
		{ptype: "p", values: []string{"super_admin", "1", "*"}},
		{ptype: "g", values: []string{"20", "super_admin", "1"}},
		{ptype: "p", values: []string{"role_reader", "2", "system:user:query"}},
		{ptype: "g", values: []string{"30", "role_reader", "2"}},
	} {
		if err := manager.AddPolicyIfAbsent(seed.ptype, seed.values...); err != nil {
			t.Fatalf("seed %s %v: %v", seed.ptype, seed.values, err)
		}
	}
}

func TestAuthorizerEnforcementIsolation(t *testing.T) {
	t.Parallel()

	authorizer, err := New(testDB(t))
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}

	seedBasePolicies(t, authorizer)

	ctx := context.Background()

	ok, err := authorizer.EnforcePlatform(ctx, "10", "platform:tenant:query")
	if err != nil || !ok {
		t.Fatalf("platform admin enforce platform = %v, %v", ok, err)
	}

	ok, err = authorizer.IsPlatformAdmin(ctx, "10")
	if err != nil || !ok {
		t.Fatalf("platform admin grouping = %v, %v", ok, err)
	}

	ok, err = authorizer.Enforce(ctx, "20", "1", "system:user:update")
	if err != nil || !ok {
		t.Fatalf("tenant super admin enforce tenant = %v, %v", ok, err)
	}

	ok, err = authorizer.EnforcePlatform(ctx, "20", "platform:tenant:query")
	if err != nil {
		t.Fatalf("tenant user platform enforce err: %v", err)
	}
	if ok {
		t.Fatal("tenant user unexpectedly gained platform access")
	}

	ok, err = authorizer.Enforce(ctx, "20", "2", "system:user:update")
	if err != nil {
		t.Fatalf("tenant user cross-tenant enforce err: %v", err)
	}
	if ok {
		t.Fatal("tenant user unexpectedly gained cross-tenant access")
	}

	ok, err = authorizer.Enforce(ctx, "30", "2", "system:user:query")
	if err != nil || !ok {
		t.Fatalf("tenant reader enforce query = %v, %v", ok, err)
	}
}

func TestPolicyManagerOperations(t *testing.T) {
	t.Parallel()

	authorizer, err := New(testDB(t))
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}

	ctx := context.Background()

	if err := authorizer.RebuildRolePolicies("role_editor", "9", []string{
		"system:user:query",
		"system:user:update",
	}); err != nil {
		t.Fatalf("rebuild role policies: %v", err)
	}
	if err := authorizer.RebuildUserGrouping("77", "9", []string{"role_editor"}); err != nil {
		t.Fatalf("rebuild user grouping: %v", err)
	}

	ok, err := authorizer.Enforce(ctx, "77", "9", "system:user:update")
	if err != nil || !ok {
		t.Fatalf("enforce rebuilt policy = %v, %v", ok, err)
	}

	if err := authorizer.RebuildRolePolicies("role_editor", "9", []string{"system:user:list"}); err != nil {
		t.Fatalf("replace role policies: %v", err)
	}

	ok, err = authorizer.Enforce(ctx, "77", "9", "system:user:update")
	if err != nil {
		t.Fatalf("enforce removed policy err: %v", err)
	}
	if ok {
		t.Fatal("stale role policy still enforced")
	}

	ok, err = authorizer.Enforce(ctx, "77", "9", "system:user:list")
	if err != nil || !ok {
		t.Fatalf("enforce replaced policy = %v, %v", ok, err)
	}

	if err := authorizer.RemoveUserGrouping("77", "9"); err != nil {
		t.Fatalf("remove user grouping: %v", err)
	}
	ok, err = authorizer.Enforce(ctx, "77", "9", "system:user:list")
	if err != nil {
		t.Fatalf("enforce removed grouping err: %v", err)
	}
	if ok {
		t.Fatal("removed user grouping still enforced")
	}

	if err := authorizer.RebuildUserGrouping("77", "9", []string{"role_editor"}); err != nil {
		t.Fatalf("rebuild user grouping again: %v", err)
	}
	if err := authorizer.RemoveRoleGrouping("role_editor", "9"); err != nil {
		t.Fatalf("remove role grouping: %v", err)
	}
	ok, err = authorizer.Enforce(ctx, "77", "9", "system:user:list")
	if err != nil {
		t.Fatalf("enforce removed role grouping err: %v", err)
	}
	if ok {
		t.Fatal("removed role grouping still enforced")
	}

	if err := authorizer.RemoveRolePolicies("role_editor", "9"); err != nil {
		t.Fatalf("remove role policies: %v", err)
	}
}
