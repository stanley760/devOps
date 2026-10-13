// huma 错误信封文案（业务呈现层）：决策与链组合在 devops-infra/security/humakit，
// 本文件只保留本服务的中文文案与信封工厂，经 Envelope 注入套件。
//
// 文案与状态码逐条对齐历史 gin 版行为：
//   - 认证失败：401「未登录或会话已过期」；会话失效：401「登录状态已失效，请重新登录」
//   - 认证基础设施错误：fail-closed 保持 401 并记录底层错误
//   - 租户不可用：403「租户不可用（停用或已过期）」
//   - 域内鉴权拒绝：403「无操作权限」（含基础设施错误，fail-closed）
//   - 平台非管理员：403「需要平台管理员权限」
package middleware

import (
	"errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/stanley760/devOps/devops-infra/http/response"
	securitymw "github.com/stanley760/devOps/devops-infra/security/middleware"
	"log/slog"
	"net/http"
)

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

// 信封回调装配（文案与状态码逐字对齐既有行为）。
var (
	writeHuma401           = writeEnvelopeMapped(http.StatusUnauthorized, "未登录或会话已过期", errMsg{securitymw.ErrSessionInactive, "登录状态已失效，请重新登录"})
	writeHumaAuthInfraErr  = writeEnvelope(http.StatusUnauthorized, "登录状态已失效，请重新登录", "认证基础设施错误")
	writeHuma403Tenant     = writeEnvelope(http.StatusForbidden, "租户不可用（停用或已过期）", "")
	writeHuma403Perm       = writeEnvelope(http.StatusForbidden, "无操作权限", "")
	writeHuma403Platform   = writeEnvelopeMapped(http.StatusForbidden, "无操作权限", errMsg{securitymw.ErrNotPlatformAdmin, "需要平台管理员权限"})
	writeHumaAuthzInfraErr = writeEnvelope(http.StatusForbidden, "无操作权限", "鉴权基础设施错误")
)
