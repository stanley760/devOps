// 数据权限解析中间件：JWT/Tenant/Perm 之后、handler 之前，一次性解析当前用户的
// 行级可见范围注入 ctx（dao.Apply / service.Visible 消费，见 internal/datascope）。
//
// 解析依据（每请求一次，缓存 60s）：
//   - 平台管理员（g2）与租户超管（角色 code=super_admin）→ 全部数据，与其功能权限分层一致
//   - 其余按用户在租户内的角色 data_scope 合并（最宽松单角色生效）
//
// 缓存与失效：进程内 TTL 缓存（角色/用户角色的保存路径调用 InvalidateDataScope 清空）。
// 不放进 JWT claims——否则改角色要等重新登录才生效；多实例部署时应改为带版本号的
// 共享缓存（store.Cache），单实例下进程内即够且少一次网络往返。
package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/dao"
	"devops-system/internal/datascope"
)

// superAdminRole 租户超管角色 code（种子策略 p, super_admin, {tid}, *，域内全通）。
const superAdminRole = "super_admin"

// dataScopeTTL 解析结果缓存时长：变更失效的兜底上限。
const dataScopeTTL = 60 * time.Second

// scopeCache 条目：(tenantID, userID) → 解析结果 + 过期时间。
type scopeCache struct {
	mu      sync.RWMutex
	entries map[[2]int64]scopeEntry
}

type scopeEntry struct {
	scope   datascope.Scope
	expires time.Time
}

var dataScopes = &scopeCache{entries: make(map[[2]int64]scopeEntry)}

// HumaDataScope 数据权限解析中间件（挂在受控资源路由：users 模块）。
// 解析失败 fail-closed：以空 Scope（不可见任何行）放行——功能权限（Perm）已经
// 把过一道门，这里失败只影响数据范围，不放大为全量可见。
func HumaDataScope() func(huma.Context, func(huma.Context)) {
	return func(hctx huma.Context, next func(huma.Context)) {
		claims := ClaimsFrom(hctx.Context())
		if claims == nil || claims.UserID == 0 {
			next(huma.WithValue(hctx, datascope.CtxKey{}, emptyScope()))
			return
		}
		scope, err := resolveDataScope(hctx.Context(), claims)
		if err != nil {
			// fail-closed：空集放行，请求继续（列表会得到 1=0 过滤或空可见性）
			next(huma.WithValue(hctx, datascope.CtxKey{}, emptyScope()))
			return
		}
		next(huma.WithValue(hctx, datascope.CtxKey{}, scope))
	}
}

// InvalidateDataScope 清空数据权限缓存（角色保存/删除、用户角色变更时调用）。
func InvalidateDataScope() {
	dataScopes.mu.Lock()
	dataScopes.entries = make(map[[2]int64]scopeEntry)
	dataScopes.mu.Unlock()
}

// datascopeCtxKey huma.WithValue 用的私有键（huma 中间件无法直接改 context.Context，
// 与 withClientIP 的键模式一致）。
type datascopeCtxKey struct{}

// emptyScope 不可见任何行的空权限（fail-closed 兜底值）。
func emptyScope() datascope.Scope { return datascope.Scope{} }

// resolveDataScope 带 TTL 缓存的解析：平台管理员/租户超管直通，否则合并角色口径。
func resolveDataScope(ctx context.Context, claims *auth.AccessClaims) (datascope.Scope, error) {
	key := [2]int64{claims.TenantID, claims.UserID}

	dataScopes.mu.RLock()
	if e, ok := dataScopes.entries[key]; ok && time.Now().Before(e.expires) {
		dataScopes.mu.RUnlock()
		return e.scope, nil
	}
	dataScopes.mu.RUnlock()

	scope, err := computeDataScope(ctx, claims)
	if err != nil {
		return datascope.Scope{}, err
	}

	dataScopes.mu.Lock()
	dataScopes.entries[key] = scopeEntry{scope: scope, expires: time.Now().Add(dataScopeTTL)}
	dataScopes.mu.Unlock()
	return scope, nil
}

// computeDataScope 一次解析（DB 三查 + Casbin 一查，均走既有 dao/authz 出口）。
func computeDataScope(ctx context.Context, claims *auth.AccessClaims) (datascope.Scope, error) {
	// 平台管理员：跨租户全通（g2 判定，与鉴权中间件同源）
	if authz.IsPlatformAdmin(claims.UserID) {
		return datascope.Scope{All: true}, nil
	}

	roles, err := dao.GetUserRoles(ctx, claims.UserID)
	if err != nil {
		return datascope.Scope{}, err
	}
	roleScopes := make([]datascope.RoleScope, 0, len(roles))
	for _, r := range roles {
		if r.Code == superAdminRole {
			return datascope.Scope{All: true}, nil // 租户超管：域内全通
		}
		roleScopes = append(roleScopes, datascope.RoleScope{
			Code:             r.Code,
			DataScope:        r.DataScope,
			DataScopeDeptIDs: r.DataScopeDeptIDs,
		})
	}

	u, err := dao.GetUserByID(ctx, claims.UserID)
	if err != nil {
		return datascope.Scope{}, err
	}

	var deptNodes []datascope.DeptNode
	if depts, err := dao.ListDepts(ctx, nil); err == nil {
		deptNodes = make([]datascope.DeptNode, 0, len(depts))
		for _, d := range depts {
			deptNodes = append(deptNodes, datascope.DeptNode{ID: d.ID, ParentID: d.ParentID})
		}
	} else {
		return datascope.Scope{}, err
	}

	return datascope.Merge(roleScopes, u.DeptID, u.ID, datascope.ChildrenMap(deptNodes)), nil
}
