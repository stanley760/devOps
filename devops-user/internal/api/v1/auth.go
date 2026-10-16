// Package v1 是 C 端 HTTP 接口层（huma handler）。
//
// 分层约定：handler 只做「绑定参数 → 调 service → 包信封」三件事，
// 不含业务判断。信封复用 devops-infra/http/response，与 devops-system 逐字对齐，
// 前端可用同一套解析逻辑。
//
// ⚠️ 错误出口必须经 errs.ToHumaError 收口（humaerr.Install 已在 main 装配）：
// service 返回的 *errs.BizError **未实现** huma.StatusError（无 GetStatus），
// 直接 return 会被 huma 当作未知错误兜底成 500，丢掉业务码与文案。
package v1

import (
	"context"
	"net/http"

	commonResp "github.com/stanley760/devops/devops-common/response"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-user/internal/auth"
	"github.com/stanley760/devops/devops-user/internal/service"
)

// AuthHandler 认证接口。
type AuthHandler struct {
	svc *service.Auth
}

// NewAuthHandler 构造认证 handler。
func NewAuthHandler(svc *service.Auth) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// registerBody 注册入参。
//
// ⚠️ huma 的字段规则（读 schema.go:895-922得出，两个易踩的坑）：
//  1. 默认**所有字段都是必填**（FieldsOptionalByDefault 默认关闭）；
//  2. `required` 是**独立 struct tag**，不是 json tag 的一部分——
//     写成 `json:"email,required=false"` 无效，huma 会从 `required` tag 单独取值。
//
// 故可选字段必须写 `json:"email" required:"false"`，或用 `json:",omitempty"`。
type registerBody struct {
	Username string `json:"username" required:"true" doc:"登录名" minLength:"3" maxLength:"32" example:"alice"`
	Password string `json:"password" required:"true" doc:"密码，至少 8 位" minLength:"8" example:"S3cret-pass"`
	Nickname string `json:"nickname" required:"false" doc:"昵称，留空则同登录名" maxLength:"32"`
	Email    string `json:"email" required:"false" doc:"邮箱" example:"alice@example.com"`
	Mobile   string `json:"mobile" required:"false" doc:"手机号"`
}

// authOutput 认证响应体。
type authOutput struct {
	Body commonResp.BaseResponse[service.TokenPair]
}

// Register POST /user/api/v1/auth/register —— 注册并直接签发 token。
func (h *AuthHandler) Register(ctx context.Context, in *struct {
	Body registerBody
}) (*authOutput, error) {
	pair, err := h.svc.Register(ctx, service.RegisterInput{
		Username: in.Body.Username,
		Password: in.Body.Password,
		Nickname: in.Body.Nickname,
		Email:    in.Body.Email,
		Mobile:   in.Body.Mobile,
	})
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &authOutput{Body: commonResp.BaseResponse[service.TokenPair]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: pair,
	}}, nil
}

// loginBody 登录入参。
type loginBody struct {
	Username string `json:"username" doc:"登录名" required:"true" example:"alice"`
	Password string `json:"password" doc:"密码" required:"true" example:"S3cret-pass"`
}

// Login POST /user/api/v1/auth/login —— 账号密码登录。
func (h *AuthHandler) Login(ctx context.Context, in *struct {
	Body loginBody
}) (*authOutput, error) {
	pair, err := h.svc.Login(ctx, ClientIPFrom(ctx), in.Body.Username, in.Body.Password)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &authOutput{Body: commonResp.BaseResponse[service.TokenPair]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: pair,
	}}, nil
}

// logoutBody 登出入参。
type logoutBody struct {
	// RefreshToken 选填：带上则一并作废该 refresh（否则旧 refresh 仍可换新 token）。
	RefreshToken string `json:"refresh_token" required:"false" doc:"要作废的刷新令牌（选填）"`
}

// Logout POST /user/api/v1/auth/logout —— 撤销当前会话。
func (h *AuthHandler) Logout(ctx context.Context, in *struct {
	Body logoutBody
}) (*authOutput, error) {
	// 身份只来自 token 中间件注入的 claims，**不接受**请求参数指定用户。
	claims := auth.ClaimsFrom(ctx)
	if err := h.svc.Logout(ctx, claims, in.Body.RefreshToken); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &authOutput{Body: commonResp.BaseResponse[service.TokenPair]{
		Code: http.StatusOK,
		Msg:  "ok",
	}}, nil
}

// refreshBody 刷新入参。
type refreshBody struct {
	RefreshToken string `json:"refresh_token" required:"true" doc:"登录时下发的刷新令牌" maxLength:"128"`
}

// Refresh POST /user/api/v1/auth/refresh —— 用 refresh token 换新双 token（轮换）。
//
// ⚠️ refresh 端点必须匿名（调用方此刻 access 已过期），故不放 kit.Logged()。
// 其安全性来自 refresh 本身是不透明随机串 + 服务端哈希校验，且旧 token 用后即废。
func (h *AuthHandler) Refresh(ctx context.Context, in *struct {
	Body refreshBody
}) (*authOutput, error) {
	pair, err := h.svc.Refresh(ctx, in.Body.RefreshToken)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &authOutput{Body: commonResp.BaseResponse[service.TokenPair]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: pair,
	}}, nil
}
