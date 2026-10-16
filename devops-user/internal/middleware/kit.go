// Package middleware 的业务装配层：把 devops-infra/security/humakit 的中间件套件
// 与本服务的中文信封文案接线。
//
// 分层：决策与链组合在 humakit（infra），本包只提供业务文案与最小绑定。
// 装配点在 cmd/root.go。
package middleware

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/stanley760/devops/devops-infra/http/response"
	"github.com/stanley760/devops/devops-infra/security/humakit"
	securitymw "github.com/stanley760/devops/devops-infra/security/middleware"

	"github.com/stanley760/devops/devops-user/internal/auth"
)

// 类型别名：router 层拿到的是 *middleware.Kit，与 infra 同型，
// 因此不需要「业务 Kit 包装 infra Kit」的间接层。
type (
	Kit  = humakit.Kit
	Deps = humakit.Deps
)

// NewKit 装配本服务中间件套件。
//
// 刻意不设置以下项（C 端无对应概念，勿照搬 devops-system）：
//   - Authorizer / SuperAdminRole / IsPlatformAdmin：无 RBAC 与平台管理员；
//     权限是「只能操作自己的数据」，由 service 层按 claims.UID 做行级过滤。
//   - TenantUsable / UserRoles / User / Depts：无租户模型，也不读后台组织架构。
//
// 若将来确实需要权限点体系，再按需注入 Authorizer，不要预先占位。
func NewKit(authn *auth.Manager, deps Deps) *Kit {
	deps.Envelope = cEndEnvelope()
	return humakit.NewKit(authn, deps)
}

// cEndEnvelope 本服务的中文信封文案。
//
// 文案按 C 端语气调整（「你」而非「操作」），避免照搬后台管理端措辞。
func cEndEnvelope() humakit.Envelope {
	return humakit.Envelope{
		// 未登录 / 会话已失效（含 Redis 会话态校验失败）
		Unauthorized: writeEnvelopeMapped(http.StatusUnauthorized, "未登录或登录已过期",
			errMsg{securitymw.ErrSessionInactive, "登录状态已失效，请重新登录"}),
		// 认证基础设施错误：fail-closed 保持 401 并记录底层错误
		AuthnInfra: writeEnvelope(http.StatusUnauthorized, "登录状态已失效，请重新登录", "认证基础设施错误"),
		// 以下三槽位 C 端不会触发（未使用 Tenant / Perm / Platform 中间件），
		// 仍给出合理文案兜底，避免 nil 回调导致 panic。
		TenantForbidden:   writeEnvelope(http.StatusForbidden, "无权访问该资源", ""),
		PermForbidden:     writeEnvelope(http.StatusForbidden, "无权访问该资源", ""),
		PlatformForbidden: writeEnvelope(http.StatusForbidden, "无权访问该资源", ""),
		AuthzInfra:        writeEnvelope(http.StatusForbidden, "无权访问该资源", "鉴权基础设施错误"),
	}
}

// errMsg 按 errors.Is 匹配错误值并给出对应文案。
type errMsg struct {
	match error
	msg   string
}

// writeEnvelope 固定状态码与文案的信封回调工厂；logKey 非空时先以 slog.Error 记录底层错误。
func writeEnvelope(status int, msg, logKey string) func(huma.Context, error) {
	return func(hctx huma.Context, err error) {
		if logKey != "" {
			slog.Error(logKey, "err", err)
		}
		response.WriteHumaEnvelope(hctx, status, msg)
	}
}

// writeEnvelopeMapped 按 err 值选文案（errors.Is 遍历，wrapped error 亦可命中；默认 defaultMsg）。
func writeEnvelopeMapped(status int, defaultMsg string, cases ...errMsg) func(huma.Context, error) {
	return func(hctx huma.Context, err error) {
		msg := defaultMsg
		for _, c := range cases {
			if errors.Is(err, c.match) {
				msg = c.msg
				break
			}
		}
		response.WriteHumaEnvelope(hctx, status, msg)
	}
}
