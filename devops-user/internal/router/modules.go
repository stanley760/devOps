package router

import (
	"github.com/danielgtaylor/huma/v2"

	apiv1 "github.com/stanley760/devops/devops-user/internal/api/v1"
	"github.com/stanley760/devops/devops-user/internal/middleware"
	"github.com/stanley760/devops/devops-user/internal/service"
)

// userAPIPrefix C 端接口对外前缀。与 devops-system 的 /system 区分，
// 便于网关侧按前缀分流与分别限流。
const userAPIPrefix = "/user"

// 匿名端点全路径。必须由本包导出、且与下方 huma 注册共用同一份定义，
// 避免与 deploy/higress/ingresses/devops-user-public.yaml 或守卫放行名单
// 各写一份字符串而悄悄漂移（漂移后果是登录被锁死）。
//
// 这些端点必须匿名：调用方此时还没有 token。C 端 jwt-auth 必须挂在独立
// ingress 上（插件按 Ingress 资源挂载、不支持路径匹配），应用层守卫也须同步放行。
const (
	// AuthLoginPath 登录端点，签发 access token。
	AuthLoginPath = userAPIPrefix + "/api/v1/auth/login"
	// AuthRegisterPath 注册端点。
	AuthRegisterPath = userAPIPrefix + "/api/v1/auth/register"
	// AuthRefreshPath 刷新端点。必须匿名：调用方此刻 access 已过期，
	// 若强制携带 JWT 则永远换不到新 token（与 login 同理）。
	AuthRefreshPath = userAPIPrefix + "/api/v1/auth/refresh"
	// JWKSPath 验签公钥，供其他服务自验签与人工排查。
	JWKSPath = "/.well-known/jwks.json"
)

// AuthRoutes 认证相关路由（登录 / 注册）。
type AuthRoutes func(huma.API)

// ProfileRoutes 用户资料路由（读取 / 修改当前用户）。
type ProfileRoutes func(huma.API)

// OAuthRoutes 三方登录路由。
type OAuthRoutes func(huma.API)

// Modules 是应用层需要显式组合的业务路由模块集合。
//
// 用 func(huma.API) 而非直接调 handler：让 router 包不依赖具体 Handler 类型，
// 模块可独立测试与替换。
type Modules struct {
	Auth    AuthRoutes
	Profile ProfileRoutes
	OAuth   OAuthRoutes
}

// NewAuthRoutes 由 service 构造认证路由。
func NewAuthRoutes(svc *service.Auth, kit *middleware.Kit) AuthRoutes {
	h := apiv1.NewAuthHandler(svc)
	return func(api huma.API) { registerAuth(api, h, kit) }
}

// NewProfileRoutes 由 service 构造用户资料路由。
func NewProfileRoutes(svc *service.Profile, kit *middleware.Kit) ProfileRoutes {
	h := apiv1.NewProfileHandler(svc)
	return func(api huma.API) { registerProfile(api, h, kit) }
}

// NewOAuthRoutes 由 service 构造三方登录路由。
func NewOAuthRoutes(svc *service.OAuth, kit *middleware.Kit) OAuthRoutes {
	h := apiv1.NewOAuthHandler(svc)
	return func(api huma.API) { registerOAuth(api, h, kit) }
}
