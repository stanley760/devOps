package humakit

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humagin "github.com/danielgtaylor/huma/v2/adapters/humagin"

	commonaudit "github.com/stanley760/devops/devops-common/audit"
	"github.com/stanley760/devops/devops-common/auditctx"
	"github.com/stanley760/devops/devops-infra/observability"
	infratenant "github.com/stanley760/devops/devops-infra/tenant"
)

// ModuleResolver 把路由的 OperationID 解析为审计模块。
//
// 返回 ok=false 表示该路由不纳入审计（如健康检查、只读列表）。
// 业务侧持有这张映射表，从而无需在每个 handler 里手动埋点。
type ModuleResolver func(operationID string) (module, subModule string, ok bool)

// AuditDeps 审计采集的业务注入点。
type AuditDeps struct {
	// Sink 审计写入端；nil → commonaudit.Noop（记录被安全丢弃，不影响业务）。
	Sink commonaudit.Sink
	// Resolve 模块解析器；nil → 不采集任何路由（默认关闭）。
	Resolve ModuleResolver
}

// NewAuditMiddleware 构造操作审计采集中间件。
//
// 设计要点：
//   - 采集范围由业务侧的 ModuleResolver 白名单决定，默认不采集任何路由；
//     业务侧通常只标注写操作，避免 GET 列表造成审计风暴。
//   - 认证信息从 ctx 取（由 Deps.WithClaims 注入的 auditctx.Actor），
//     租户从 ctx 取（由 securityTenant 注入），故本中间件须挂在
//     认证与租户中间件之后。
//   - 审计写入是旁路：Sink.Emit 不返回错误，失败不影响业务响应。
//
// 调用方须把返回值赋给 Kit（见 kit.enableAudit）；未启用审计的业务服务
// 使用内置的 noopAudit，不会因 nil 中间件崩溃（huma 的链组合不跳过 nil）。
func (k *Kit) NewAuditMiddleware(deps AuditDeps) Middleware {
	sink := deps.Sink
	if sink == nil {
		sink = commonaudit.Noop
	}
	resolve := deps.Resolve

	return func(hctx huma.Context, next func(huma.Context)) {
		op := hctx.Operation()
		var module, subModule string
		audited := false
		if resolve != nil && op != nil {
			module, subModule, audited = resolve(op.OperationID)
		}
		if !audited {
			next(hctx)
			return
		}

		meta := clientMetaOf(hctx)
		rec := commonaudit.Record{
			Module:        module,
			SubModule:     subModule,
			RequestMethod: hctx.Method(),
			RequestURL:    hctx.URL().Path,
			TraceID:       observability.TraceIDFromContext(hctx.Context()),
			UserIP:        meta.IP,
			UserAgent:     meta.UserAgent,
			GeoCountry:    meta.GeoCountry,
			GeoProvince:   meta.GeoProvince,
			GeoCity:       meta.GeoCity,
			GeoISP:        meta.GeoISP,
		}
		applyIdentity(&rec, hctx.Context())

		startedAt := time.Now()
		next(hctx)

		rec.Code = hctx.Status()
		if rec.Code >= 400 {
			rec.Result = commonaudit.ResultFailure
		} else {
			rec.Result = commonaudit.ResultSuccess
		}
		rec.Extra = durationExtra(time.Since(startedAt))

		sink.Emit(hctx.Context(), rec)
	}
}

// noopAudit 是未启用审计时的占位中间件：原样放行。
//
// huma 的 Middlewares.chain 不跳过 nil 元素，故各业务链必须挂一个非 nil
// 中间件，用本占位实现表达"本服务不采集审计"这一显式决策。
func noopAudit(hctx huma.Context, next func(huma.Context)) { next(hctx) }

// applyIdentity 从 ctx 补齐操作者与租户信息。
//
// 两处来源都是可选的：未认证路由（如公开接口）没有 actor，
// 全局表操作没有租户——缺失时保留零值，不阻断采集。
func applyIdentity(rec *commonaudit.Record, ctx context.Context) {
	if actor, ok := auditctx.From(ctx); ok {
		rec.UserID = actor.UserID
		rec.Username = actor.Username
		rec.UserType = commonaudit.UserType(actor.UserType)
	}
	if tid, ok := infratenant.TenantID(ctx); ok {
		rec.TenantID = tid
	}
}

// clientMetaOf 提取客户端元数据并按契约列宽截断。
func clientMetaOf(hctx huma.Context) commonaudit.ClientMeta {
	meta := commonaudit.ClientMeta{
		UserAgent: hctx.Header("User-Agent"),
	}
	// 复用 gin 适配器的 ClientIP（按可信代理配置解析 X-Forwarded-For）；
	// 非 gin 适配器回退为直接读 XFF 首段。
	if g := humagin.Unwrap(hctx); g != nil {
		meta.IP = g.ClientIP()
	} else {
		xff := hctx.Header("X-Forwarded-For")
		if idx := strings.IndexByte(xff, ','); idx > 0 {
			xff = xff[:idx]
		}
		meta.IP = strings.TrimSpace(xff)
	}
	// 网关 geo-ip 插件注入的头按 URL 转义写入，此处还原；
	// 非法转义序列用原值兜底——审计不因脏头失败。
	unescape := func(name string) string {
		raw := hctx.Header(name)
		if raw == "" {
			return ""
		}
		v, err := url.QueryUnescape(raw)
		if err != nil {
			return raw
		}
		return v
	}
	meta.GeoCountry = unescape("X-Higress-Geo-Country")
	meta.GeoProvince = unescape("X-Higress-Geo-Province")
	meta.GeoCity = unescape("X-Higress-Geo-City")
	meta.GeoISP = unescape("X-Higress-Geo-Isp")

	return commonaudit.ClipClientMeta(meta)
}

// durationExtra 渲染耗时扩展字段。
func durationExtra(d time.Duration) string {
	return `{"duration_ms":` + strconv.FormatInt(d.Milliseconds(), 10) + `}`
}