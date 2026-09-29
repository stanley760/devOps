// 认证接口（huma）。Input/Output 结构体即文档 schema，
// 错误经 errs.ToHumaError 收口为统一信封（humaerr.Install 已在 main 装配）。
package api

import (
	"context"

	"devops-infra/errs"
	"devops-infra/http/response"

	"devops-system/internal/auth"
	"devops-system/internal/service"
	"devops-system/internal/types"
)

// AuthHandler 认证接口。
type AuthHandler struct {
	svc *service.Auth
}

// NewAuthHandler 创建 handler。
func NewAuthHandler(svc *service.Auth) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// ---- Input / Output（结构体即 OpenAPI schema）----

// LoginInput POST /api/v1/auth/login。
type LoginInput struct {
	Body types.LoginReq
}

// LoginOutput 登录响应信封。
type LoginOutput struct {
	Body response.BaseResponse[types.LoginResp]
}

// RefreshInput POST /api/v1/auth/refresh。
type RefreshInput struct {
	Body types.RefreshReq
}

// RefreshOutput 刷新响应信封。
type RefreshOutput struct {
	Body response.BaseResponse[types.TokenResp]
}

// LogoutOutput 登出响应信封（无数据）。
type LogoutOutput struct {
	Body response.BaseResponse[any]
}

// InfoOutput GET /auth/info 响应信封。
type InfoOutput struct {
	Body response.BaseResponse[types.UserInfoResp]
}

// RoutersOutput GET /auth/routers 响应信封。
type RoutersOutput struct {
	Body response.BaseResponse[[]types.RouterNode]
}

// UpdateProfileInput PUT /auth/profile。
type UpdateProfileInput struct {
	Body types.UpdateProfileReq
}

// UpdateProfileOutput 更新个人信息响应信封（无数据）。
type UpdateProfileOutput struct {
	Body response.BaseResponse[any]
}

// UpdatePasswordInput PUT /auth/profile/password。
type UpdatePasswordInput struct {
	Body types.UpdatePasswordReq
}

// UpdatePasswordOutput 修改密码响应信封（无数据，改密踢下线）。
type UpdatePasswordOutput struct {
	Body response.BaseResponse[any]
}

// ---- Handlers ----

// Login POST /api/v1/auth/login（clientIP 经 withClientIP 中间件注入）。
func (h *AuthHandler) Login(ctx context.Context, in *LoginInput) (*LoginOutput, error) {
	resp, err := h.svc.Login(ctx, in.Body, ClientIPFrom(ctx))
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &LoginOutput{Body: response.OKBody(*resp)}, nil
}

// Refresh POST /api/v1/auth/refresh（轮换 refresh 会话）。
func (h *AuthHandler) Refresh(ctx context.Context, in *RefreshInput) (*RefreshOutput, error) {
	resp, err := h.svc.Refresh(ctx, in.Body)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RefreshOutput{Body: response.OKBody(*resp)}, nil
}

// Logout POST /api/v1/auth/logout（需 JWT）。
func (h *AuthHandler) Logout(ctx context.Context, _ *struct{}) (*LogoutOutput, error) {
	h.svc.Logout(ctx, auth.ClaimsFrom(ctx))
	return &LogoutOutput{Body: response.EmptyBody()}, nil
}

// Info GET /api/v1/auth/info（需 JWT + Tenant）。
func (h *AuthHandler) Info(ctx context.Context, _ *struct{}) (*InfoOutput, error) {
	resp, err := h.svc.Info(ctx, auth.ClaimsFrom(ctx))
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &InfoOutput{Body: response.OKBody(*resp)}, nil
}

// Routers GET /api/v1/auth/routers（需 JWT + Tenant）。
func (h *AuthHandler) Routers(ctx context.Context, _ *struct{}) (*RoutersOutput, error) {
	routers, err := h.svc.Routers(ctx, auth.ClaimsFrom(ctx))
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &RoutersOutput{Body: response.OKBody(routers)}, nil
}

// UpdateProfile PUT /api/v1/auth/profile（需 JWT + Tenant）。
func (h *AuthHandler) UpdateProfile(ctx context.Context, in *UpdateProfileInput) (*UpdateProfileOutput, error) {
	if err := h.svc.UpdateProfile(ctx, auth.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UpdateProfileOutput{Body: response.EmptyBody()}, nil
}

// UpdatePassword PUT /api/v1/auth/profile/password（需 JWT + Tenant，改密踢下线）。
func (h *AuthHandler) UpdatePassword(ctx context.Context, in *UpdatePasswordInput) (*UpdatePasswordOutput, error) {
	if err := h.svc.UpdatePassword(ctx, auth.ClaimsFrom(ctx), in.Body); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &UpdatePasswordOutput{Body: response.EmptyBody()}, nil
}

// ClientIPContextKey 登录客户端 IP 在 context 中的键（withClientIP 中间件注入）。
type ClientIPContextKey struct{}

// ClientIPFrom 取登录请求的客户端 IP（withClientIP 之后可用）。
func ClientIPFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ClientIPContextKey{}).(string); ok {
		return v
	}
	return ""
}
