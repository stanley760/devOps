// huma 版租户上下文中间件（D2/D7）。
package tenant

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/stanley760/devOps/devops-infra/http/response"
	securitymw "github.com/stanley760/devOps/devops-infra/security/middleware"
)

// HumaTenantContext claims → check 回调 → 租户上下文注入。
//
// 安全关键——传播机制：必须用 huma.WithContext 整体替换底层 context（等价
// gin 版 c.Request = c.Request.WithContext(ctx)）；contextKey 未导出，只能经
// 本包 With 构造，严禁自造 key。check 回调由业务注入（如 devops-system 的
// tenantUsable：DB 状态查询 + Redis 缓存）；onUnauthorized/onForbidden 为
// nil 时以统一信封终止（与 securitymw 默认一致）。
func HumaTenantContext(
	check func(ctx context.Context, tenantID int64) bool,
	onUnauthorized func(hctx huma.Context, err error),
	onForbidden func(hctx huma.Context, err error),
) func(huma.Context, func(huma.Context)) {
	if onUnauthorized == nil {
		onUnauthorized = func(hctx huma.Context, _ error) {
			response.WriteHumaEnvelope(hctx, http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))
		}
	}
	if onForbidden == nil {
		onForbidden = func(hctx huma.Context, _ error) {
			response.WriteHumaEnvelope(hctx, http.StatusForbidden, http.StatusText(http.StatusForbidden))
		}
	}
	return func(hctx huma.Context, next func(huma.Context)) {
		claims := securitymw.HumaCurrentClaims(hctx.Context())
		if claims == nil {
			onUnauthorized(hctx, securitymw.ErrUnauthorized)
			return
		}
		if !check(hctx.Context(), claims.TenantID) {
			onForbidden(hctx, securitymw.ErrForbidden)
			return
		}
		next(huma.WithContext(hctx, With(hctx.Context(), claims.TenantID, claims.TenantCode)))
	}
}
