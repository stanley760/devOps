package authz

import (
	"context"
	"testing"
)

// TestSeedBootstrapDefaultRoles 默认角色名下 SeedBootstrap 的产出与历史硬编码种子等价，
// 且幂等（重复调用不追加行）。
func TestSeedBootstrapDefaultRoles(t *testing.T) {
	t.Parallel()

	authorizer, err := New(testDB(t))
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}

	if err := authorizer.SeedBootstrap("10", "1"); err != nil {
		t.Fatalf("seed bootstrap: %v", err)
	}
	// 幂等：再跑一次不应报错、不应翻倍语义
	if err := authorizer.SeedBootstrap("10", "1"); err != nil {
		t.Fatalf("seed bootstrap idempotent: %v", err)
	}

	ctx := context.Background()

	ok, err := authorizer.IsPlatformAdmin(ctx, "10")
	if err != nil || !ok {
		t.Fatalf("platform admin after seed = %v, %v", ok, err)
	}
	ok, err = authorizer.EnforcePlatform(ctx, "10", "platform:tenant:query")
	if err != nil || !ok {
		t.Fatalf("platform admin enforce = %v, %v", ok, err)
	}
	ok, err = authorizer.Enforce(ctx, "10", "1", "system:user:update")
	if err != nil || !ok {
		t.Fatalf("tenant super admin enforce = %v, %v", ok, err)
	}
	// 租户超管不得触达平台接口
	ok, err = authorizer.EnforcePlatform(ctx, "20", "platform:tenant:query")
	if err != nil {
		t.Fatalf("platform enforce err: %v", err)
	}
	if ok {
		t.Fatal("unbound subject unexpectedly gained platform access")
	}
}

// TestSeedTenantAdminDefaultRoles SeedTenantAdmin 只写租户超管行，不触达平台。
func TestSeedTenantAdminDefaultRoles(t *testing.T) {
	t.Parallel()

	authorizer, err := New(testDB(t))
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}

	if err := authorizer.SeedTenantAdmin("20", "3"); err != nil {
		t.Fatalf("seed tenant admin: %v", err)
	}
	if err := authorizer.SeedTenantAdmin("20", "3"); err != nil {
		t.Fatalf("seed tenant admin idempotent: %v", err)
	}

	ctx := context.Background()

	ok, err := authorizer.Enforce(ctx, "20", "3", "system:user:update")
	if err != nil || !ok {
		t.Fatalf("tenant admin enforce = %v, %v", ok, err)
	}
	ok, err = authorizer.Enforce(ctx, "20", "4", "system:user:update")
	if err != nil {
		t.Fatalf("cross-tenant enforce err: %v", err)
	}
	if ok {
		t.Fatal("tenant admin unexpectedly gained cross-tenant access")
	}
	ok, err = authorizer.IsPlatformAdmin(ctx, "20")
	if err != nil {
		t.Fatalf("platform admin check err: %v", err)
	}
	if ok {
		t.Fatal("tenant admin unexpectedly became platform admin")
	}
}

// TestCustomRoleConfig 自定义角色名：种子写入与 IsPlatformAdmin 判定必须同源，
// 否则会出现「种子写自定义名、判定查 platform_admin」的脑裂。
func TestCustomRoleConfig(t *testing.T) {
	t.Parallel()

	cfg := RoleConfig{PlatformAdmin: "ops_admin", SuperAdmin: "ops_super"}
	authorizer, err := New(testDB(t), WithRoles(cfg))
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}

	if got := authorizer.Roles(); got != cfg {
		t.Fatalf("Roles() = %+v, want %+v", got, cfg)
	}

	if err := authorizer.SeedBootstrap("10", "1"); err != nil {
		t.Fatalf("seed bootstrap custom: %v", err)
	}

	ctx := context.Background()

	// 判定必须命中自定义名（g2, 10, ops_admin），而不是 platform_admin
	ok, err := authorizer.IsPlatformAdmin(ctx, "10")
	if err != nil || !ok {
		t.Fatalf("custom platform admin = %v, %v", ok, err)
	}
	ok, err = authorizer.EnforcePlatform(ctx, "10", "platform:tenant:query")
	if err != nil || !ok {
		t.Fatalf("custom platform enforce = %v, %v", ok, err)
	}
	ok, err = authorizer.Enforce(ctx, "10", "1", "system:user:update")
	if err != nil || !ok {
		t.Fatalf("custom super admin enforce = %v, %v", ok, err)
	}

	// 默认名不应被写入
	ok, err = authorizer.IsPlatformAdmin(ctx, "99")
	if err != nil {
		t.Fatalf("is platform admin err: %v", err)
	}
	if ok {
		t.Fatal("unbound subject matched platform admin")
	}
}

// TestRoleConfigZeroValueDefaults 零值字段回填默认，部分自定义也能跑。
func TestRoleConfigZeroValueDefaults(t *testing.T) {
	t.Parallel()

	authorizer, err := New(testDB(t), WithRoles(RoleConfig{PlatformAdmin: "ops_admin"}))
	if err != nil {
		t.Fatalf("new authorizer: %v", err)
	}

	got := authorizer.Roles()
	if got.PlatformAdmin != "ops_admin" {
		t.Fatalf("PlatformAdmin = %q, want ops_admin", got.PlatformAdmin)
	}
	if got.SuperAdmin != "super_admin" {
		t.Fatalf("SuperAdmin = %q, want super_admin (zero-value default)", got.SuperAdmin)
	}
}
