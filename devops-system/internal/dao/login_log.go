// 登录审计：每次登录尝试（成功与失败）各落一行，供审计追溯。
package dao

import (
	"context"
	"log/slog"

	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
)

// CreateLoginLog 落一行登录审计。尽力而为：审计写入失败不影响登录，
// 仅 slog 告警（与 TouchLogin 同语义，但审计丢行需要可见性）。
// 调用方须已注入租户 ctx（TenantPlugin fail-closed，无租户 ctx 会报错并被此处吞掉）。
func CreateLoginLog(ctx context.Context, log *model.LoginLog) {
	if log == nil {
		return
	}
	if err := tenantdb.DB(ctx).Create(log).Error; err != nil {
		slog.WarnContext(ctx, "登录审计落库失败", "username", log.Username, "err", err)
	}
}
