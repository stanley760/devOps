package authz

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	securityauthz "devops-infra/security/authz"
)

// testDB 内存库（casbin_rule 由 gorm-adapter 自动建表）。
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func setup(t *testing.T) {
	t.Helper()
	a, err := securityauthz.New(testDB(t))
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}
	SetAuthorizer(a)
	t.Cleanup(func() { Close() })
}

func TestEnforceTenantIsolationAndPlatformAdmin(t *testing.T) {
	setup(t)

	// 与 migrate 种子同形：admin(1) = 平台管理员 + 租户1超管
	if err := SeedPolicies(1, 1); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if !Enforce(1, 1, "system:user:query") {
		t.Fatal("tenant super admin should enforce in-domain permission")
	}
	if !EnforcePlatform(1, "platform:tenant:query") {
		t.Fatal("platform admin should enforce platform permission")
	}
	if !IsPlatformAdmin(1) {
		t.Fatal("user 1 should be platform admin")
	}

	// 租户用户（无角色绑定）域内拒绝、无法触达平台权限
	if err := SeedTenantAdmin(2, 20); err != nil {
		t.Fatalf("seed tenant admin: %v", err)
	}
	if !Enforce(20, 2, "system:user:query") {
		t.Fatal("tenant 2 super admin should enforce in tenant 2")
	}
	if Enforce(20, 1, "system:user:query") {
		t.Fatal("cross-tenant enforce must be denied")
	}
	if EnforcePlatform(20, "platform:tenant:query") {
		t.Fatal("tenant admin must not reach platform permissions")
	}
	if IsPlatformAdmin(20) {
		t.Fatal("user 20 must not be platform admin")
	}
}

func TestPolicyOperationsRoundTrip(t *testing.T) {
	setup(t)

	if err := RebuildRolePolicies("editor", 3, []string{"system:user:query", "system:user:update"}); err != nil {
		t.Fatalf("rebuild role: %v", err)
	}
	if err := RebuildUserGrouping(30, 3, []string{"editor"}); err != nil {
		t.Fatalf("rebuild grouping: %v", err)
	}
	if !Enforce(30, 3, "system:user:update") {
		t.Fatal("rebuilt policy should enforce")
	}

	if err := RebuildRolePolicies("editor", 3, []string{"system:user:query"}); err != nil {
		t.Fatalf("replace role: %v", err)
	}
	if Enforce(30, 3, "system:user:update") {
		t.Fatal("stale permission still enforced")
	}
	if !Enforce(30, 3, "system:user:query") {
		t.Fatal("replaced permission should enforce")
	}

	if err := RemoveUserGrouping(30, 3); err != nil {
		t.Fatalf("remove grouping: %v", err)
	}
	if Enforce(30, 3, "system:user:query") {
		t.Fatal("removed grouping still enforced")
	}

	if err := RebuildUserGrouping(30, 3, []string{"editor"}); err != nil {
		t.Fatalf("re-add grouping: %v", err)
	}
	if err := RemoveRoleGrouping("editor", 3); err != nil {
		t.Fatalf("remove role grouping: %v", err)
	}
	if Enforce(30, 3, "system:user:query") {
		t.Fatal("role grouping removal did not take effect")
	}

	if err := RemoveRolePolicies("editor", 3); err != nil {
		t.Fatalf("remove role policies: %v", err)
	}
}

func TestFailClosedWhenUninitialized(t *testing.T) {
	Close() // 确保无实例

	if Enforce(1, 1, "system:user:query") {
		t.Fatal("uninitialized enforce must fail closed")
	}
	if EnforcePlatform(1, "platform:tenant:query") {
		t.Fatal("uninitialized platform enforce must fail closed")
	}
	if IsPlatformAdmin(1) {
		t.Fatal("uninitialized platform-admin check must fail closed")
	}
	if err := RebuildRolePolicies("r", 1, nil); err == nil {
		t.Fatal("uninitialized policy op must return error")
	}
	if err := SeedPolicies(1, 1); err == nil {
		t.Fatal("uninitialized seed must return error")
	}
}
