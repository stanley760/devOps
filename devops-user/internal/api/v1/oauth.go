package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	commonResp "github.com/stanley760/devops/devops-common/response"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-user/internal/auth"
	"github.com/stanley760/devops/devops-user/internal/service"
)

// OAuthHandler 三方登录接口。
type OAuthHandler struct {
	svc *service.OAuth
}

// NewOAuthHandler 构造 OAuth handler。
func NewOAuthHandler(svc *service.OAuth) *OAuthHandler {
	return &OAuthHandler{svc: svc}
}

// providerOutput 三方提供方状态。
type providerOutput struct {
	Body commonResp.BaseResponse[[]service.ProviderStatus]
}

// ListProviders GET /user/api/v1/oauth/providers —— 列出可用提供方与授权地址。
//
// 匿名：用户还没登录，正是要选登录方式。
func (h *OAuthHandler) ListProviders(ctx context.Context, _ *struct{}) (*providerOutput, error) {
	list, err := h.svc.ListProviders(ctx)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &providerOutput{Body: commonResp.BaseResponse[[]service.ProviderStatus]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: list,
	}}, nil
}

// bindBody 主动绑定入参。
type bindBody struct {
	Provider string `json:"provider" required:"true" doc:"提供方（github / google / wechat）" maxLength:"16"`
	// RedirectAfter 登录后跳回的前端地址。**服务端会做白名单校验**，
	// 未校验会造成 open redirect（钓鱼）。
	RedirectAfter string `json:"redirect_after,required=false" doc:"登录后跳转地址（须在白名单内）" maxLength:"512"`
}

// startBindData 授权地址数据。
type startBindData struct {
	AuthorizeURL string `json:"authorize_url" doc:"三方授权跳转地址"`
}

// startBindOutput 授权地址。
type startBindOutput struct {
	Body commonResp.BaseResponse[startBindData]
}

// StartBind POST /user/api/v1/oauth/bind/start —— 已登录用户发起绑定。
//
// 需登录：绑定是「把三方账号挂到当前登录用户」的动作。
func (h *OAuthHandler) StartBind(ctx context.Context, in *struct {
	Body bindBody
}) (*startBindOutput, error) {
	claims := auth.ClaimsFrom(ctx)
	if claims == nil {
		return nil, errs.ToHumaError(errs.ErrUnauthorized)
	}
	url, err := h.svc.StartBind(ctx, in.Body.Provider, in.Body.RedirectAfter, claims.UserID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &startBindOutput{Body: commonResp.BaseResponse[startBindData]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: startBindData{AuthorizeURL: url},
	}}, nil
}

// callbackOutput 回调结果。
type callbackOutput struct {
	Body commonResp.BaseResponse[*service.OAuthResult]
}

// ⚠️ **不能把参数放进嵌入结构体**：huma 的 findParams 对 f.Anonymous 直接 `return nil`
// （不递归进去），故嵌入类型的 `path` / `query` / `form` 标签**完全不生效**——
// 表现为 path 参数恒为空、query 参数收不到。必须直接声明在 handler 的入参结构体里。
// 该行为见 huma@v2.39.1/huma.go:263-266。

// Callback GET /user/api/v1/oauth/{provider}/callback —— 三方授权回调（query 模式）。
//
// **必须匿名**：三方跳转回来时用户尚未拿到本地 token。
// 安全性靠 state一次性校验（防 CSRF），不靠登录态。
func (h *OAuthHandler) Callback(ctx context.Context, in *struct {
	Provider string `path:"provider" doc:"提供方标识"`
	Code     string `query:"code" doc:"三方返回的授权码"`
	State    string `query:"state" doc:"防 CSRF 的 state"`
	OAuthErr string `query:"error" doc:"三方返回的错误码（用户取消授权时会有）"`
}) (*callbackOutput, error) {
	return h.handleCallback(ctx, in.Provider, in.Code, in.State, in.OAuthErr)
}

// CallbackPost POST /user/api/v1/oauth/{provider}/callback —— form_post 模式回调。
//
// 与 GET 版分开而非合并：huma 会要求「含 body 参数的结构体必须有请求体」，
// 合并会导致 query 模式的请求直接报 "request body is required"。
// GitHub / Google / 微信均用 query（GET）回传，此端点为后续 form_post 类
// 提供方预留（若接入时须同步更新 router 注释与匿名路径清单）。
func (h *OAuthHandler) CallbackPost(ctx context.Context, in *struct {
	Body struct {
		Provider string `path:"provider" doc:"提供方标识"`
		Code     string `form:"code" doc:"三方返回的授权码"`
		State    string `form:"state" doc:"防 CSRF 的 state"`
		OAuthErr string `form:"error" doc:"三方返回的错误码"`
	}
}) (*callbackOutput, error) {
	return h.handleCallback(ctx, in.Body.Provider, in.Body.Code, in.Body.State, in.Body.OAuthErr)
}

// handleCallback 两模式的共同处理逻辑。
func (h *OAuthHandler) handleCallback(ctx context.Context, provider, code, state, oerr string) (*callbackOutput, error) {
	if provider == "" {
		return nil, errs.ToHumaError(errs.New(errs.CodeParam, "缺少 provider"))
	}
	// 用户在三方那边主动取消授权时也会回调，带 error 参数。
	if oerr != "" {
		msg := "三方授权失败"
		if oerr == "access_denied" {
			msg = "你已取消授权"
		}
		return nil, errs.ToHumaError(errs.New(errs.CodeParam, msg))
	}
	res, err := h.svc.Callback(ctx, provider, code, state)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &callbackOutput{Body: commonResp.BaseResponse[*service.OAuthResult]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: res,
	}}, nil
}

// unbindOutput 解绑结果。
type unbindOutput struct {
	Body commonResp.BaseResponse[any]
}

// Unbind DELETE /user/api/v1/oauth/bind —— 解绑三方账号。
//
// 需登录：只能解绑自己的绑定。
//
// ⚠️ 解绑后若该用户**只剩三方登录方式**（无本地口令可用），将无法登录。
// 故当前实现要求：本地用户名口令始终存在（自动建号的用户也有随机强口令），
// 解绑不影响其重新登录。
func (h *OAuthHandler) Unbind(ctx context.Context, in *struct {
	Provider string `query:"provider" required:"true" doc:"要解绑的提供方" maxLength:"16"`
}) (*unbindOutput, error) {
	claims := auth.ClaimsFrom(ctx)
	if claims == nil {
		return nil, errs.ToHumaError(errs.ErrUnauthorized)
	}
	if err := h.svc.Unbind(ctx, claims.UserID, in.Provider); err != nil {
		return nil, errs.ToHumaError(err)
	}
	return &unbindOutput{Body: commonResp.BaseResponse[any]{
		Code: http.StatusOK,
		Msg:  "已解绑",
	}}, nil
}

// listBoundOutput 绑定列表。
type listBoundOutput struct {
	Body commonResp.BaseResponse[[]oauthBoundView]
}

// oauthBoundView 绑定视图（不含任何 token）。
type oauthBoundView struct {
	Provider  string `json:"provider" doc:"提供方"`
	Nickname  string `json:"nickname" doc:"三方昵称快照"`
	AvatarURL string `json:"avatar_url" doc:"三方头像快照"`
	CreatedAt int64  `json:"created_at" doc:"绑定时间（Unix 毫秒）"`
}

// ListBound GET /user/api/v1/oauth/bind —— 列已绑定的三方账号。
func (h *OAuthHandler) ListBound(ctx context.Context, _ *struct{}) (*listBoundOutput, error) {
	claims := auth.ClaimsFrom(ctx)
	if claims == nil {
		return nil, errs.ToHumaError(errs.ErrUnauthorized)
	}
	list, err := h.svc.ListBound(ctx, claims.UserID)
	if err != nil {
		return nil, errs.ToHumaError(err)
	}
	out := make([]oauthBoundView, 0, len(list))
	for _, m := range list {
		out = append(out, oauthBoundView{
			Provider:  string(m.Provider),
			Nickname:  m.Nickname,
			AvatarURL: m.AvatarURL,
			CreatedAt: m.CreateTime.UnixMilli(),
		})
	}
	return &listBoundOutput{Body: commonResp.BaseResponse[[]oauthBoundView]{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: out,
	}}, nil
}

// 确保 huma 被引用（本文件用它的 schema 标签）。
var _ = huma.Operation{}
