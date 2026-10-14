// Package middleware 的业务装配层：把 devops-infra/security/humakit 的中间件链
// 与本服务的文案、数据源、审计、租户语义接线。
//
// 分层：决策与链组合在 humakit（infra），本包只做业务默认绑定——
// 中文信封文案（huma.go）、租户可用性判定（tenant.go）、
// dao 适配层（dao/scope_source.go）、auditctx 注入。装配点在 cmd/root.go。
package middleware

import (
	"context"

	"github.com/stanley760/devops/devops-common/audit"
	"github.com/stanley760/devops/devops-common/auditctx"
	client "github.com/stanley760/devops/devops-infra/database/mysql"
	"github.com/stanley760/devops/devops-infra/security/humakit"
	securityjwt "github.com/stanley760/devops/devops-infra/security/jwt"

	"github.com/stanley760/devops/devops-system/internal/auth"
	"github.com/stanley760/devops/devops-system/internal/authz"
	"github.com/stanley760/devops/devops-system/internal/dao"
	"github.com/stanley760/devops/devops-system/internal/store"
)

// 类型别名：router/app/wire 的 *middleware.Kit 签名零改动，wire 无需重新生成。
type (
	Kit  = humakit.Kit
	Deps = humakit.Deps
)

// NewKit 装配业务中间件套件：nil/零值项补生产默认绑定（业务语义），
// 信封文案与数据源适配均留在本层，决策与链组合在 devops-infra/security/humakit。
func NewKit(authn *auth.Manager, deps Deps) *Kit {
	if deps.Authorizer == nil {
		deps.Authorizer = authz.Authorizer()
	}
	if deps.SuperAdminRole == "" {
		deps.SuperAdminRole = authz.Roles().SuperAdmin
	}
	if deps.TenantUsable == nil {
		deps.TenantUsable = newTenantChecker(client.Get(), store.Cache())
	}
	if deps.IsPlatformAdmin == nil {
		deps.IsPlatformAdmin = func(_ context.Context, userID int64) bool { return authz.IsPlatformAdmin(userID) }
	}
	if deps.UserRoles == nil {
		deps.UserRoles = dao.UserScopes
	}
	if deps.User == nil {
		deps.User = dao.UserRef
	}
	if deps.Depts == nil {
		deps.Depts = dao.DeptNodes
	}
	if deps.WithClaims == nil {
		deps.WithClaims = injectAuditActor
	}
	deps.Envelope = systemEnvelope()
	return humakit.NewKit(authn, deps)
}

// injectAuditActor 认证成功后把身份注入审计上下文：
// model.BeforeCreate/BeforeUpdate 消费其中的用户名填creator/updater，
// humakit 审计采集中间件消费完整身份写入审计记录。
// B 端管理后台的操作者统一标记为 audit.UserTypeAdmin。
func injectAuditActor(ctx context.Context, claims *securityjwt.Claims) context.Context {
	if claims == nil || claims.Username == "" {
		return ctx
	}
	return auditctx.WithActor(ctx, auditctx.Actor{
		UserID:   claims.UID,
		Username: claims.Username,
		UserType: int64(audit.UserTypeAdmin),
		TenantID: claims.TenantID,
	})
}

// systemEnvelope 本服务的中文信封文案（六个槽位与历史 gin 版逐字对齐）。
func systemEnvelope() humakit.Envelope {
	return humakit.Envelope{
		Unauthorized:      writeHuma401,
		AuthnInfra:        writeHumaAuthInfraErr,
		TenantForbidden:   writeHuma403Tenant,
		PermForbidden:     writeHuma403Perm,
		PlatformForbidden: writeHuma403Platform,
		AuthzInfra:        writeHumaAuthzInfraErr,
	}
}
