// operations.go devops-system 全部业务接口的 huma 注册（按模块拆分）。
// gin 与 huma 同路径不能共存：模块迁移时必须先删 router.go 对应 gin 注册块再在此注册。
package router

import (
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	humagin "github.com/danielgtaylor/huma/v2/adapters/humagin"

	"devops-infra/http/humaerr"

	apiv1 "devops-system/internal/api/v1"
	"devops-system/internal/middleware"
	"devops-system/internal/service"
)

const systemAPIPrefix = "/system"

// with 合并公共 Operation 字段并添加 devops-system 的接口前缀。
func with(common huma.Operation, op huma.Operation) huma.Operation {
	op.Path = systemAPIPrefix + op.Path
	if op.Tags == nil {
		op.Tags = common.Tags
	}
	if op.Responses == nil {
		op.Responses = common.Responses
	}
	return op
}

// withClientMeta 登录接口提取客户端元数据（经 gin 适配器解包；可信代理已配置时
// ClientIP 为真实 IP）。Geo 四项来自 higress geo-ip 插件注入的 X-Higress-Geo-* 头
// （插件按 URL 转义写入，此处还原），随登录落 t_system_login_log 审计表。
func withClientMeta(hctx huma.Context, next func(huma.Context)) {
	c := humagin.Unwrap(hctx)
	geo := func(name string) string {
		v, err := url.QueryUnescape(c.GetHeader(name))
		if err != nil { // 非法转义序列：用原值兜底，审计不因脏头失败
			return c.GetHeader(name)
		}
		return v
	}
	next(huma.WithValue(hctx, apiv1.ClientMetaContextKey{}, service.LoginMeta{
		IP:          c.ClientIP(),
		UserAgent:   c.GetHeader("User-Agent"),
		GeoCountry:  geo("X-Higress-Geo-Country"),
		GeoProvince: geo("X-Higress-Geo-Province"),
		GeoCity:     geo("X-Higress-Geo-City"),
		GeoIsp:      geo("X-Higress-Geo-Isp"),
	}))
}

// registerPosts posts 模块 5 接口（P2，迁移 SOP 样板）。
func registerPosts(api huma.API, h *apiv1.PostHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"posts"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "post-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/posts",
		Summary:     "岗位列表",
		Middlewares: kit.Biz("system:post:list"),
	}), h.List)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "post-get",
		Method:      http.MethodGet,
		Path:        "/api/v1/posts/{id}",
		Summary:     "岗位详情",
		Middlewares: kit.Biz("system:post:query"),
	}), h.Get)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "post-create",
		Method:      http.MethodPost,
		Path:        "/api/v1/posts",
		Summary:     "创建岗位",
		Description: "code 创建后不可改。",
		Middlewares: kit.Biz("system:post:create"),
	}), h.Create)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "post-update",
		Method:      http.MethodPut,
		Path:        "/api/v1/posts/{id}",
		Summary:     "更新岗位",
		Middlewares: kit.Biz("system:post:update"),
	}), h.Update)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "post-delete",
		Method:      http.MethodDelete,
		Path:        "/api/v1/posts/{id}",
		Summary:     "删除岗位",
		Middlewares: kit.Biz("system:post:delete"),
	}), h.Delete)
}

// registerRoles roles 模块 6 接口（P3）。
func registerRoles(api huma.API, h *apiv1.RoleHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"roles"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "role-list", Method: http.MethodGet, Path: "/api/v1/roles",
		Summary: "角色列表", Middlewares: kit.Biz("system:role:list"),
	}), h.List)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "role-get", Method: http.MethodGet, Path: "/api/v1/roles/{id}",
		Summary: "角色详情", Middlewares: kit.Biz("system:role:query"),
	}), h.Get)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "role-create", Method: http.MethodPost, Path: "/api/v1/roles",
		Summary: "创建角色", Description: "code 创建后不可改。",
		Middlewares: kit.Biz("system:role:create"),
	}), h.Create)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "role-update", Method: http.MethodPut, Path: "/api/v1/roles/{id}",
		Summary: "更新角色", Middlewares: kit.Biz("system:role:update"),
	}), h.Update)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "role-assign-menus", Method: http.MethodPut, Path: "/api/v1/roles/{id}/menus",
		Summary: "分配角色菜单", Description: "重建 role_menu 与角色 p 行快照。",
		Middlewares: kit.Biz("system:role:update"),
	}), h.AssignMenus)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "role-delete", Method: http.MethodDelete, Path: "/api/v1/roles/{id}",
		Summary: "删除角色", Middlewares: kit.Biz("system:role:delete"),
	}), h.Delete)
}

// registerMenus menus 模块 5 接口（P3）。
func registerMenus(api huma.API, h *apiv1.MenuHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"menus"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "menu-list", Method: http.MethodGet, Path: "/api/v1/menus",
		Summary: "菜单树", Middlewares: kit.Biz("system:menu:list"),
	}), h.List)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "menu-get", Method: http.MethodGet, Path: "/api/v1/menus/{id}",
		Summary: "菜单详情", Middlewares: kit.Biz("system:menu:query"),
	}), h.Get)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "menu-create", Method: http.MethodPost, Path: "/api/v1/menus",
		Summary: "创建菜单", Middlewares: kit.Biz("system:menu:create"),
	}), h.Create)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "menu-update", Method: http.MethodPut, Path: "/api/v1/menus/{id}",
		Summary: "更新菜单", Description: "permission 变更联动重建引用角色 p 行。",
		Middlewares: kit.Biz("system:menu:update"),
	}), h.Update)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "menu-delete", Method: http.MethodDelete, Path: "/api/v1/menus/{id}",
		Summary: "删除菜单", Middlewares: kit.Biz("system:menu:delete"),
	}), h.Delete)
}

// registerDepts depts 模块 5 接口（P3）。
func registerDepts(api huma.API, h *apiv1.DeptHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"depts"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "dept-list", Method: http.MethodGet, Path: "/api/v1/depts",
		Summary: "部门树", Middlewares: kit.Biz("system:dept:list"),
	}), h.List)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "dept-get", Method: http.MethodGet, Path: "/api/v1/depts/{id}",
		Summary: "部门详情", Middlewares: kit.Biz("system:dept:query"),
	}), h.Get)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "dept-create", Method: http.MethodPost, Path: "/api/v1/depts",
		Summary: "创建部门", Middlewares: kit.Biz("system:dept:create"),
	}), h.Create)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "dept-update", Method: http.MethodPut, Path: "/api/v1/depts/{id}",
		Summary: "更新部门", Middlewares: kit.Biz("system:dept:update"),
	}), h.Update)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "dept-delete", Method: http.MethodDelete, Path: "/api/v1/depts/{id}",
		Summary: "删除部门", Middlewares: kit.Biz("system:dept:delete"),
	}), h.Delete)
}

// registerUsers users 模块 6 接口（P4；行级数据权限受控资源，走 scopedBiz 链）。
func registerUsers(api huma.API, h *apiv1.UserHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"users"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-list", Method: http.MethodGet, Path: "/api/v1/users",
		Summary: "用户列表", Middlewares: kit.ScopedBiz("system:user:list"),
	}), h.List)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-get", Method: http.MethodGet, Path: "/api/v1/users/{id}",
		Summary: "用户详情", Middlewares: kit.ScopedBiz("system:user:query"),
	}), h.Get)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-create", Method: http.MethodPost, Path: "/api/v1/users",
		Summary: "创建用户", Description: "角色分配联动 Casbin g 行。",
		Middlewares: kit.ScopedBiz("system:user:create"),
	}), h.Create)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-update", Method: http.MethodPut, Path: "/api/v1/users/{id}",
		Summary: "更新用户", Middlewares: kit.ScopedBiz("system:user:update"),
	}), h.Update)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-reset-password", Method: http.MethodPut, Path: "/api/v1/users/{id}/password",
		Summary: "重置用户密码", Description: "重置后踢下线该用户全部会话。",
		Middlewares: kit.ScopedBiz("system:user:update-password"),
	}), h.ResetPassword)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "user-delete", Method: http.MethodDelete, Path: "/api/v1/users/{id}",
		Summary: "删除用户", Middlewares: kit.ScopedBiz("system:user:delete"),
	}), h.Delete)
}

// registerLoginLogs 登录审计模块 3 接口（查询侧；写入侧随登录 defer 落库）。
// 删除/清空为留存量管控入口，独立权限；行按租户隔离（TenantPlugin 注入 tenant_id）。
func registerLoginLogs(api huma.API, h *apiv1.LoginLogHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"login-logs"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "loginlog-list",
		Method:      http.MethodGet,
		Path:        "/api/v1/login-logs",
		Summary:     "登录日志列表",
		Description: "登录审计分页查询（成功与失败尝试各一行），支持用户名/IP/结果/时间范围筛选。",
		Middlewares: kit.Biz("system:loginlog:list"),
	}), h.List)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "loginlog-delete",
		Method:      http.MethodDelete,
		Path:        "/api/v1/login-logs",
		Summary:     "删除登录日志",
		Description: "按 ID 批量物理删除（ids 逗号分隔，如 ?ids=12,34,56）。",
		Middlewares: kit.Biz("system:loginlog:delete"),
	}), h.Delete)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "loginlog-clean",
		Method:      http.MethodDelete,
		Path:        "/api/v1/login-logs/clean",
		Summary:     "清空登录日志",
		Description: "清空本租户全部登录审计行（留存量管控，独立权限门控）。",
		Middlewares: kit.Biz("system:loginlog:clean"),
	}), h.Clean)
}

// registerTenants tenants 模块 6 接口（P5，平台路由 kit.plat）。
func registerTenants(api huma.API, h *apiv1.TenantHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"tenants"},
		Responses: humaerr.CommonErrResponses(),
	}

	huma.Register(api, with(common, huma.Operation{
		OperationID: "tenant-list", Method: http.MethodGet, Path: "/api/v1/tenants",
		Summary: "租户列表", Middlewares: kit.PlatChain("platform:tenant:list"),
	}), h.List)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "tenant-get", Method: http.MethodGet, Path: "/api/v1/tenants/{id}",
		Summary: "租户详情", Middlewares: kit.PlatChain("platform:tenant:query"),
	}), h.Get)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "tenant-create", Method: http.MethodPost, Path: "/api/v1/tenants",
		Summary: "创建租户", Description: "含租户初始超管。",
		Middlewares: kit.PlatChain("platform:tenant:create"),
	}), h.Create)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "tenant-update", Method: http.MethodPut, Path: "/api/v1/tenants/{id}",
		Summary: "更新租户", Middlewares: kit.PlatChain("platform:tenant:update"),
	}), h.Update)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "tenant-update-status", Method: http.MethodPut, Path: "/api/v1/tenants/{id}/status",
		Summary: "租户启停", Description: "停用即全员下线。",
		Middlewares: kit.PlatChain("platform:tenant:update"),
	}), h.UpdateStatus)
	huma.Register(api, with(common, huma.Operation{
		OperationID: "tenant-delete", Method: http.MethodDelete, Path: "/api/v1/tenants/{id}",
		Summary: "删除租户", Description: "默认租户不可删；需先停用。",
		Middlewares: kit.PlatChain("platform:tenant:delete"),
	}), h.Delete)
}

// registerAuth auth 模块 7 接口（P1）。
func registerAuth(api huma.API, h *apiv1.AuthHandler, kit *middleware.Kit) {
	common := huma.Operation{
		Tags:      []string{"auth"},
		Responses: humaerr.CommonErrResponses(),
	}
	logged := kit.Logged()
	biz := kit.TenantBiz()

	huma.Register(api, with(common, huma.Operation{
		OperationID: "auth-login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "登录",
		Description: "校验租户与用户后签发 access token 与 refresh token。",
		Security:    []map[string][]string{}, // 公开接口覆盖全局安全要求
		Middlewares: huma.Middlewares{withClientMeta},
	}), h.Login)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "auth-refresh",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/refresh",
		Summary:     "刷新令牌",
		Description: "轮换 refresh 会话并签发新的双 token。",
		Security:    []map[string][]string{},
	}), h.Refresh)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "auth-logout",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/logout",
		Summary:     "登出",
		Description: "撤销当前会话（踢下线）。",
		Middlewares: logged,
	}), h.Logout)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "auth-info",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/info",
		Summary:     "当前用户信息",
		Description: "用户、租户、角色编码与权限标识。",
		Middlewares: biz,
	}), h.Info)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "auth-routers",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/routers",
		Summary:     "前端路由树",
		Description: "按用户菜单权限生成的前端路由节点树。",
		Middlewares: biz,
	}), h.Routers)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "auth-update-profile",
		Method:      http.MethodPut,
		Path:        "/api/v1/auth/profile",
		Summary:     "更新个人信息",
		Middlewares: biz,
	}), h.UpdateProfile)

	huma.Register(api, with(common, huma.Operation{
		OperationID: "auth-update-password",
		Method:      http.MethodPut,
		Path:        "/api/v1/auth/profile/password",
		Summary:     "修改密码",
		Description: "校验旧密码后修改，并撤销该用户全部会话（重新登录）。",
		Middlewares: biz,
	}), h.UpdatePassword)
}
