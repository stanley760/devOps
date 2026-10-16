// operations.go devops-user 全部业务接口的 huma 注册（按模块拆分）。
//
// 与 devops-system 的差异（有意为之）：
//   - 前缀为 /user 而非 /system；
//   - 不带 TenantHeader（OpenAPI 里的 X-Tenant-ID）——C 端无租户上下文；
//   - 鉴权用 kit.JWT()（仅校验身份），**不用** kit.Perm(...)：
//     C 端权限是「只能操作自己的数据」，属行级归属校验，在 service 层
//     按 claims.UID 过滤，而非 RBAC 权限点。
package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/stanley760/devops/devops-infra/http/humaerr"

	apiv1 "github.com/stanley760/devops/devops-user/internal/api/v1"
	"github.com/stanley760/devops/devops-user/internal/middleware"
)

// with 合并公共 Operation 字段并添加本服务的接口前缀。
func with(common huma.Operation, op huma.Operation) huma.Operation {
	op.Path = userAPIPrefix + op.Path
	if op.Tags == nil {
		op.Tags = common.Tags
	}
	if op.Responses == nil {
		op.Responses = common.Responses
	}
	return op
}

// registerAuth 注册认证模块接口。
func registerAuth(api huma.API, h *apiv1.AuthHandler, kit *middleware.Kit) {
	// 客户端 IP 注入 ctx：登录限流需要，且必须走可信代理解析（防伪造 XFF）。
	ipMw := huma.Middlewares{apiv1.ClientIPMiddleware}
	common := huma.Operation{
		Tags:      []string{"auth"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-register",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/register",
		Summary:     "注册",
		Description: "创建 C 端账号并签发 access + refresh token。",
		Security:    []map[string][]string{}, // 公开接口覆盖全局安全要求
		Middlewares: ipMw,
	}), h.Register)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "登录",
		Description: "校验账号密码后签发 access + refresh token。连续失败会触发限流与账号锁定。",
		Security:    []map[string][]string{},
		Middlewares: ipMw,
	}), h.Login)

	// ⚠️ refresh 必须匿名：调用方此刻 access 已过期，若强制携带 JWT 则永远刷不出来。
	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-refresh",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/refresh",
		Summary:     "刷新令牌",
		Description: "用 refresh token 换新的 access + refresh（轮换：旧 refresh 立即失效）。",
		Security:    []map[string][]string{},
		Middlewares: ipMw,
	}), h.Refresh)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-logout",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/logout",
		Summary:     "登出",
		Description: "撤销当前会话（踢下线）。",
		Middlewares: kit.Logged(),
	}), h.Logout)
}

// registerProfile 注册用户资料模块接口（均需登录）。
func registerProfile(api huma.API, h *apiv1.ProfileHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"profile"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-profile-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/profile",
		Summary:     "获取当前用户资料",
		Middlewares: kit.Logged(),
	}), h.Get)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-password-change",
		Method:      http.MethodPut,
		Path:        "/api/v1/profile/password",
		Summary:     "修改密码",
		Description: "需校验原密码；成功后应重新登录。",
		Middlewares: kit.Logged(),
	}), h.ChangePassword)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-profile-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/profile",
		Summary:     "修改当前用户资料",
		Description: "只能修改自己的资料；越权由 service 层按 claims.UID 拦截。",
		Middlewares: kit.Logged(),
	}), h.Update)
}

// registerOAuth 注册三方登录模块接口。
//
// 匿名与鉴权分组（**这是本模块最容易出错的地方**）：
//
//	匿名：providers / callback —— 用户选登录方式、跳转回来时都还没有本地 token；
//	      安全性靠 callback 的 state 一次性校验，不靠登录态。
//	鉴权：bind / bind/start / bind/list —— 是「把三方账号挂到当前用户」的
//	      动作，必须确认操作者是登录态本人，否则可把别人的三方账号绑到自己名下。
func registerOAuth(api huma.API, h *apiv1.OAuthHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"oauth"},
		Responses: humaerr.CommonErrResponses(),
	}
	ipMw := huma.Middlewares{apiv1.ClientIPMiddleware}

	// 列出可用提供方（含各自的授权地址与一次性 state）。
	huma.Register(api, with(common, huma.Operation{
		OperationID: "oauth-providers",
		Method:      http.MethodGet,
		Path:        "/api/v1/oauth/providers",
		Summary:     "列出可用的三方登录方式",
		Middlewares: ipMw,
	}), h.ListProviders)

	// 三方回调。同一路径同时注册 GET 与 POST：
	// GitHub/Google/微信均用 query（GET）回传；POST 分支为 form_post 类提供方预留。
	// Method 必须显式给——huma.Register缺 method 会 panic（不是返回错误）。
	callbackOp := huma.Operation{
		OperationID: "oauth-callback",
		Method:      http.MethodGet,
		Path:        "/api/v1/oauth/{provider}/callback",
		Summary:     "三方授权回调",
		Description: "由三方平台重定向调用；state 一次性校验通过后按绑定情况登录或建号。",
		Security:    []map[string][]string{},
		Middlewares: ipMw,
	}
	huma.Register(api, with(common, callbackOp), h.Callback)

	callbackOpPost := callbackOp
	callbackOpPost.Method = http.MethodPost
	callbackOpPost.OperationID = "oauth-callback-post"
	callbackOpPost.Description = "form_post 模式提供方回传时走本端点（当前三家均用 GET，留作预留）。"
	huma.Register(api, with(common, callbackOpPost), h.CallbackPost)

	// 发起绑定（需登录）。
	huma.Register(api, with(common, huma.Operation{
		OperationID: "oauth-bind-start",
		Method:      http.MethodPost,
		Path:        "/api/v1/oauth/bind/start",
		Summary:     "发起绑定三方账号",
		Description: "返回三方授权地址；回调时会把三方账号绑到当前登录用户。",
		Middlewares: append(kit.Logged(), ipMw...),
	}), h.StartBind)

	// 绑定列表（需登录）。
	huma.Register(api, with(common, huma.Operation{
		OperationID: "oauth-bind-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/oauth/bind",
		Summary:     "列出已绑定的三方账号",
		Middlewares: kit.Logged(),
	}), h.ListBound)

	// 解绑（需登录）。
	huma.Register(api, with(common, huma.Operation{
		OperationID: "oauth-unbind",
		Method:      http.MethodDelete,
		Path:        "/api/v1/oauth/bind",
		Summary:     "解绑三方账号",
		Middlewares: kit.Logged(),
	}), h.Unbind)
}
