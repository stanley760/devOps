package humakit

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/stanley760/devops/devops-infra/datascope"
	securityjwt "github.com/stanley760/devops/devops-infra/security/jwt"
	securitymw "github.com/stanley760/devops/devops-infra/security/middleware"
)

// fakeAuthn 返回固定 claims 的认证器（免去 JWT/会话装配）。
type fakeAuthn struct {
	claims *securityjwt.Claims
	err    error
}

func (f fakeAuthn) Authenticate(context.Context, string) (*securityjwt.Claims, error) {
	return f.claims, f.err
}

// probeScope 组装挂 JWT + Scope 的探测路由，返回取到的 ctx Scope。
func probeScope(t *testing.T, authn fakeAuthn, deps Deps) (datascope.Scope, bool, int) {
	t.Helper()
	kit := NewKit(authn, deps)

	cfg := huma.DefaultConfig("humakit-test", "v1")
	cfg.CreateHooks = nil
	_, api := humatest.New(t, cfg)

	var got datascope.Scope
	var ok bool
	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/",
		Middlewares: huma.Middlewares{kit.JWT(), kit.Scope()},
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		got, ok = datascope.From(ctx)
		return nil, nil
	})
	return got, ok, api.Get("/").Code
}

// TestDefaultEnvelope401 未注入文案时认证失败走安全默认（HTTP 状态短语信封）。
func TestDefaultEnvelope401(t *testing.T) {
	t.Parallel()

	kit := NewKit(fakeAuthn{err: securitymw.ErrUnauthorized}, Deps{})

	cfg := huma.DefaultConfig("humakit-envelope", "v1")
	cfg.CreateHooks = nil
	_, api := humatest.New(t, cfg)
	huma.Register(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/",
		Middlewares: kit.Logged(),
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) { return nil, nil })

	resp := api.Get("/")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), http.StatusText(http.StatusUnauthorized)) {
		t.Fatalf("body = %s, want status-phrase envelope", resp.Body.String())
	}
}

// TestScopeSuperAdmin 租户超管直通全部数据。
func TestScopeSuperAdmin(t *testing.T) {
	t.Parallel()

	got, ok, code := probeScope(t, fakeAuthn{claims: &securityjwt.Claims{UID: 20, TenantID: 1}}, Deps{
		SuperAdminRole: "super_admin",
		UserRoles: func(context.Context, int64) ([]datascope.RoleScope, error) {
			return []datascope.RoleScope{{Code: "super_admin", DataScope: datascope.ScopeSelf}}, nil
		},
		User: func(context.Context, int64) (datascope.UserRef, error) {
			return datascope.UserRef{ID: 20, DeptID: 9}, nil
		},
		Depts: func(context.Context) ([]datascope.DeptNode, error) { return nil, nil },
	})
	if code != http.StatusNoContent {
		t.Fatalf("probe status = %d", code)
	}
	if !ok || !got.All {
		t.Fatalf("scope = %+v ok=%v, want All", got, ok)
	}
}

// TestScopeFailClosedWithoutSource 数据源缺失时解析失败 → 空 Scope，不得放大为全量可见。
func TestScopeFailClosedWithoutSource(t *testing.T) {
	t.Parallel()

	got, ok, code := probeScope(t, fakeAuthn{claims: &securityjwt.Claims{UID: 20, TenantID: 1}}, Deps{})
	if code != http.StatusNoContent {
		t.Fatalf("probe status = %d", code)
	}
	if !ok {
		t.Fatal("scope key not injected")
	}
	if got.All || got.SelfOnly || len(got.DeptIDs) > 0 {
		t.Fatalf("scope = %+v, want empty (fail-closed)", got)
	}
}

// TestScopeMergeDeptBelow 多角色合并：本部门及以下按部门树展开。
func TestScopeMergeDeptBelow(t *testing.T) {
	t.Parallel()

	got, ok, code := probeScope(t, fakeAuthn{claims: &securityjwt.Claims{UID: 30, TenantID: 1}}, Deps{
		SuperAdminRole:  "super_admin",
		IsPlatformAdmin: func(context.Context, int64) bool { return false },
		UserRoles: func(context.Context, int64) ([]datascope.RoleScope, error) {
			return []datascope.RoleScope{{Code: "ops", DataScope: datascope.ScopeDeptBelow}}, nil
		},
		User: func(context.Context, int64) (datascope.UserRef, error) {
			return datascope.UserRef{ID: 30, DeptID: 2}, nil
		},
		Depts: func(context.Context) ([]datascope.DeptNode, error) {
			return []datascope.DeptNode{{ID: 2, ParentID: 1}, {ID: 3, ParentID: 2}, {ID: 4, ParentID: 1}}, nil
		},
	})
	if code != http.StatusNoContent {
		t.Fatalf("probe status = %d", code)
	}
	if !ok {
		t.Fatal("scope key not injected")
	}
	// 部门 2 及其子树 {2,3}，不含兄弟部门 4
	want := map[int64]bool{2: true, 3: true}
	if len(got.DeptIDs) != len(want) {
		t.Fatalf("DeptIDs = %v, want keys %v", got.DeptIDs, want)
	}
	for _, id := range got.DeptIDs {
		if !want[id] {
			t.Fatalf("DeptIDs = %v, contains unexpected %d", got.DeptIDs, id)
		}
	}
}

// TestPlatformAdminScope 平台管理员全域直通。
func TestPlatformAdminScope(t *testing.T) {
	t.Parallel()

	got, ok, code := probeScope(t, fakeAuthn{claims: &securityjwt.Claims{UID: 1, TenantID: 1}}, Deps{
		IsPlatformAdmin: func(context.Context, int64) bool { return true },
	})
	if code != http.StatusNoContent {
		t.Fatalf("probe status = %d", code)
	}
	if !ok || !got.All {
		t.Fatalf("scope = %+v ok=%v, want All", got, ok)
	}
}

// TestInvalidateDataScope 失效钩子清空缓存。
func TestInvalidateDataScope(t *testing.T) {
	t.Parallel()

	kit := NewKit(fakeAuthn{claims: &securityjwt.Claims{UID: 1, TenantID: 1}}, Deps{})
	kit.InvalidateDataScope() // 不应 panic
}
