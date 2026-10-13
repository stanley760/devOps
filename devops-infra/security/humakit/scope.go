package humakit

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/stanley760/devOps/devops-infra/datascope"
	securitymw "github.com/stanley760/devOps/devops-infra/security/middleware"
)

// errNoScopeSource 解析数据源未注入（fail-closed：解析失败 → 空 Scope）。
var errNoScopeSource = errors.New("humakit: data scope source not configured")

// scopeCache 条目：(tenantID, userID) → 解析结果 + 过期时间（Kit 实例持有）。
// 进程内 TTL 缓存：变更失效由调用方 InvalidateDataScope 兜底，
// 多实例部署时应改为带版本号的共享缓存。
type scopeCache struct {
	mu      sync.RWMutex
	entries map[[2]int64]scopeEntry
	ttl     time.Duration
}

type scopeEntry struct {
	scope   datascope.Scope
	expires time.Time
}

// newScopeCache 构造空缓存。
func newScopeCache(ttl time.Duration) *scopeCache {
	return &scopeCache{entries: make(map[[2]int64]scopeEntry), ttl: ttl}
}

// invalidate 清空全部条目。
func (c *scopeCache) invalidate() {
	c.mu.Lock()
	c.entries = make(map[[2]int64]scopeEntry)
	c.mu.Unlock()
}

// securityScope 数据权限解析中间件（挂在受控资源路由）。
// 解析失败 fail-closed：以空 Scope（不可见任何行）放行——功能权限（Perm）已经
// 把过一道门，这里失败只影响数据范围，不放大为全量可见。
func (k *Kit) securityScope() Middleware {
	return func(hctx huma.Context, next func(huma.Context)) {
		claims := securitymw.HumaCurrentClaims(hctx.Context())
		if claims == nil || claims.UID == 0 {
			next(huma.WithValue(hctx, datascope.CtxKey{}, datascope.Scope{}))
			return
		}
		scope, err := k.resolveDataScope(hctx.Context(), claims.UID, claims.TenantID)
		if err != nil {
			// fail-closed：空集放行，请求继续（列表会得到 1=0 过滤或空可见性）
			next(huma.WithValue(hctx, datascope.CtxKey{}, datascope.Scope{}))
			return
		}
		next(huma.WithValue(hctx, datascope.CtxKey{}, scope))
	}
}

// resolveDataScope 带 TTL 缓存的解析：平台管理员/租户超管直通，否则合并角色口径。
func (k *Kit) resolveDataScope(ctx context.Context, userID, tenantID int64) (datascope.Scope, error) {
	key := [2]int64{tenantID, userID}

	k.scopes.mu.RLock()
	if e, ok := k.scopes.entries[key]; ok && time.Now().Before(e.expires) {
		k.scopes.mu.RUnlock()
		return e.scope, nil
	}
	k.scopes.mu.RUnlock()

	scope, err := k.computeDataScope(ctx, userID)
	if err != nil {
		return datascope.Scope{}, err
	}

	k.scopes.mu.Lock()
	k.scopes.entries[key] = scopeEntry{scope: scope, expires: time.Now().Add(k.scopes.ttl)}
	k.scopes.mu.Unlock()
	return scope, nil
}

// computeDataScope 一次解析（数据源 + 平台判定均经 Deps 注入，一律走 infra 值对象）。
func (k *Kit) computeDataScope(ctx context.Context, userID int64) (datascope.Scope, error) {
	deps := k.deps

	// 平台管理员：跨租户全通（g2 判定，与鉴权中间件同源）
	if deps.IsPlatformAdmin(ctx, userID) {
		return datascope.Scope{All: true}, nil
	}

	if deps.UserRoles == nil || deps.User == nil || deps.Depts == nil {
		return datascope.Scope{}, errNoScopeSource
	}

	roleScopes, err := deps.UserRoles(ctx, userID)
	if err != nil {
		return datascope.Scope{}, err
	}
	for _, r := range roleScopes {
		if r.Code == deps.SuperAdminRole {
			return datascope.Scope{All: true}, nil // 租户超管：域内全通
		}
	}

	u, err := deps.User(ctx, userID)
	if err != nil {
		return datascope.Scope{}, err
	}

	deptNodes, err := deps.Depts(ctx)
	if err != nil {
		return datascope.Scope{}, err
	}

	return datascope.Merge(roleScopes, u.DeptID, u.ID, datascope.ChildrenMap(deptNodes)), nil
}
