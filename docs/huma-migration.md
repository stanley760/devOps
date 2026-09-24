# devops-system 全量迁移 Huma 方案（工业级落地版）

> 版本：v1.2（2026-09-23，架构定稿：新增 D7 公共业务组件下沉 devops-infra；确立终态零双轨原则；P0 改 infra 优先）
> 范围：devops-system 全部 40 个业务接口（auth 7 + users 6 + roles 6 + menus 5 + depts 5 + posts 5 + tenants 6）+ 文档装配；devops-infra 做最小公共扩展。
> 结论先行：**gin 保留为底层 HTTP server，huma 全量接管路由/校验/文档三合一**，分 7 个阶段原子切换，每阶段可独立回滚。
> 分工原则：**Huma 主导业务面**——业务路由注册、参数校验、业务中间件（JWT/Tenant/Perm/PlatformPerm）、错误信封与 OpenAPI 文档；**Gin 保留为底层**——HTTP server 与基础配置、全局中间件链（Recovery/AccessLog/OTel trace/RED 指标）、基础设施端点（/healthz /readyz /metrics /.well-known/jwks.json）、NoRoute/NoMethod 信封与优雅关闭。
> 单一方案原则：终态**零双轨**——P6 后业务面不存在任何 gin 代码（路由注册/binding 校验/业务中间件/Wrap 出口全部清除）；分阶段只是实施风险控制（模块内原子切换，gin 与 huma 无同路径共存）。可复用业务组件（认证/鉴权/租户/信封的 huma 形态）全部下沉 devops-infra（见 D7），后续业务模块零成本接入。

---

## 1. 现状盘点（迁移基线）

| 项 | 现状 | 迁移后 |
|---|---|---|
| HTTP server | gin（devops-infra/http，Recovery/AccessLog/healthz/metrics/JWKS） | 不变 |
| 路由注册 | gin Group + `response.Wrap`（apidoc 开启时 38 个）+ huma（login/refresh 2 个） | huma.Register 全量 40 个 |
| 中间件 | gin 版 JWT/Tenant/Perm/PlatformPerm | huma.Middlewares 同逻辑移植 |
| 校验 | gin binding（validator） | huma schema 校验（422→400 映射回信封） |
| 响应 | `response.BaseResponse` 信封 | 同一信封，泛型直用作 Output Body |
| 错误 | `response.FailErr`（BizError→code/msg） | `errs.ToHumaError`（已存在，复用） |
| 文档 | 仅 login/refresh 入文档 | 全量入文档 |
| 依赖版本 | huma v2.39.1 / gin v1.12.0 | 不变（无需升级） |

关键有利条件（已核实源码）：
- service 层直接返回 DTO：`([]types.UserDTO, int64, error)` 等 → **Output 零适配**
- `response.BaseResponse[T]` / `PageData[T]` 泛型已存在 → 直接当 Body
- `errs.ToHumaError` 已存在且内部调用 `huma.NewError` → 覆盖 `NewError` 后自动生效
- `LoginReq`/`RefreshReq` 已有 huma 标签先例（`minLength`/`maxLength`/`format:"uuid"`）
- huma 自定义错误机制已核实（`error.go`：`StatusError` 接口 = `GetStatus()`+`Error()`，覆盖包级变量 `huma.NewError` 后自定义结构体直接序列化为响应体，且不实现 `ContentTypeFilter` 时 Content-Type 保持 `application/json`）

---

## 2. 七项核心技术决策

### D1 错误模型：覆盖 `huma.NewError`，错误响应保持统一响应形态 + 保留业务错误码

现有失败响应（gin 版）：HTTP status = code，body = `{"code":400,"msg":"...","data":null}`。
huma 默认：RFC7807 problem+json。**必须覆盖**，否则破坏前端契约。

新增 `devops-infra/http/humaerr` 包：

```go
// Package humaerr huma 错误出口与统一信封对齐。
package humaerr

// Envelope 错误信封（与 response.BaseResponse[any] 的失败形态字节一致）。
type Envelope struct {
    Code int    `json:"code"`
    Msg  string `json:"msg"`
    Data any    `json:"data"` // 恒 nil，序列化为 "data":null，与 gin 版一致
}

func (e *Envelope) GetStatus() int { return e.Code }
func (e *Envelope) Error() string  { return e.Msg }

// Install 覆盖 huma.NewError（包级变量），全局生效；须在任何 huma 请求处理前调用。
// 422（huma schema 校验失败）映射为 400，校验明细并入 msg。
func Install() {
    huma.NewError = func(status int, msg string, detailErrs ...error) huma.StatusError {
        if status == http.StatusUnprocessableEntity {
            status = errs.CodeParam // 422 → 400，与旧契约一致
            msg = formatValidation(msg, detailErrs) // "参数校验失败: body.username: ..."
        }
        return &Envelope{Code: status, Msg: msg}
    }
}
```

在 `main.go`（或 httpinfra.Run 装配处）调用 `humaerr.Install()`。此后：
- handler 返回 `errs.ToHumaError(err)` → 走 `huma.NewError` → 信封 ✔
- huma 校验失败 → 400 信封 ✔
- Content-Type 保持 `application/json`（自定义类型无 ContentTypeFilter）✔

### D2 中间件：JWT/Tenant/Perm/PlatformPerm 移植为 huma.Middlewares

gin Group 中间件对 huma 注册的路由不生效，必须移植。claims 载体从 `gin.Context.Keys` 换为 `context.Context`；
**租户上下文同理必须显式传播**——机制见下方 Tenant 注释（`huma.WithContext` 已从 v2.39.1 api.go:161 源码验证，
与 gin 版 `c.Request = c.Request.WithContext(ctx)` 语义等价）。
**实现位置按 D7 下沉 devops-infra**（认证/鉴权 → `security/middleware/huma.go`，租户 → `tenant/huma.go`，均回调注入模式）；
下方代码为移植语义参考，devops-system 侧仅保留装配胶水（HumaOptions 回调 + tenantUsable + ClaimsFrom）：

```go
// devops-system/internal/middleware/huma.go
type claimsKeyType struct{}

// ClaimsFrom 取 JWT 载荷（JWT 中间件之后可用）。
func ClaimsFrom(ctx context.Context) *auth.AccessClaims {
    c, _ := ctx.Value(claimsKeyType{}).(*auth.AccessClaims)
    return c
}

func JWT(m *auth.Manager) huma.Middleware {
    return func(hctx huma.Context, next func(huma.Context)) {
        shared, err := m.Authenticate(hctx.Context(), hctx.Header("Authorization"))
        if err != nil {
            writeEnvelope(hctx, http.StatusUnauthorized, unauthorizedMsg(err))
            return // 不调 next 即中断
        }
        next(huma.WithValue(hctx, claimsKeyType{}, auth.FromShared(shared)))
    }
}

// Tenant 移植 gin 版同逻辑（tenantUsable 复用现有实现，含 Redis 缓存 tenant:{id} 5min）。
//
// 安全关键——租户上下文传播机制：gin 版靠 c.Request = c.Request.WithContext(ctx)
// 让后续 handler/dao 取到租户；huma 版必须用 huma.WithContext 整体替换底层
// context（huma v2.39.1 api.go:161）。infratenant 的 contextKey 未导出，只能经
// infratenant.With 构造；严禁自造 key、严禁用 huma.WithValue 塞租户值——两者
// 都会让 tenantdb.DB(ctx) 取不到租户，行级过滤静默失效（跨租户数据泄露，
// 不会有任何报错）。middleware_test 必须复刻 gin 版全部租户用例并断言 dao 层过滤生效。
func Tenant() huma.Middleware {
    return func(hctx huma.Context, next func(huma.Context)) {
        claims := ClaimsFrom(hctx.Context())
        if claims == nil {
            writeEnvelope(hctx, http.StatusUnauthorized, "未登录或会话已过期")
            return
        }
        if !tenantUsable(hctx.Context(), client.Get(), claims.TenantID) {
            writeEnvelope(hctx, http.StatusForbidden, "租户不可用（停用或已过期）")
            return
        }
        next(huma.WithContext(hctx, infratenant.With(hctx.Context(), claims.TenantID, claims.TenantCode)))
    }
}

func Perm(perm string) huma.Middleware {
    return func(hctx huma.Context, next func(huma.Context)) {
        claims := ClaimsFrom(hctx.Context())
        if claims == nil { writeEnvelope(hctx, 401, "未登录或会话已过期"); return }
        if !authz.Enforce(claims.UserID, claims.TenantID, perm) {
            writeEnvelope(hctx, 403, "无操作权限"); return
        }
        next(hctx)
    }
}
// PlatformPerm 同理（g2 校验 + EnforcePlatform）

func writeEnvelope(hctx huma.Context, status int, msg string) {
    b, _ := json.Marshal(humaerr.Envelope{Code: status, Msg: msg})
    hctx.SetStatus(status)
    hctx.SetHeader("Content-Type", "application/json; charset=utf-8")
    _, _ = hctx.BodyWriter().Write(b)
}
```

业务 handler 取 claims 的方式从 `middleware.CurrentClaims(c)` 改为 `middleware.ClaimsFrom(ctx)`（ctx 即 huma handler 第一个参数，service 调用链不变）。

### D3 DTO：保留 json 字段名，补 huma 标签；types.ID 修正文档形态

**标签映射表**（覆盖本项目实际用到的全部约束）：

| validator（现） | huma（新） | 备注 |
|---|---|---|
| `required` | 字段非指针且 json 无 `omitempty` | 自动推导 |
| `omitempty`（可选） | 指针字段，或 json 加 `omitempty` | 仅请求 DTO 加；响应 DTO 禁加（改变输出） |
| `min=2,max=30`（string） | `minLength:"2" maxLength:"30"` | |
| `min=0,max=1`（数字） | `minimum:"0" maximum:"1"` | |
| `email` | `format:"email"` | |
| `uuid` | `format:"uuid"` | RefreshReq 已有 |
| `Status int8 binding:"omitempty"`（0 为有效值） | 改 `*int8` 指针 | TenantStatusReq 等处，指针化保语义 |
| 分页 `binding:"omitempty,min=1"`（PageNum/PageSize） | **不加 `minimum` 标签** | 0 值显式传入须走 dao 兜底，见下方「分页零值」决策 |

**分页零值（决策：分页字段不加 `minimum`）**：gin 版对 `?page_num=0` 经 `omitempty` 跳过校验、
由 dao 的 `pageOf` 兜底为默认页；huma 对**缺省**参数跳过校验（v2.39.1 huma.go:985 对空值提前返回，
已源码核实），但对**显式传入**的参数执行 schema 校验——若加 `minimum:"1"`，`page_num=0` 会被 422→400，
破坏旧契约。故 PageNum/PageSize 及同类「0 值合法、dao 兜底」的分页/筛选字段一律不加 `minimum`；
其余业务数值字段（Status 0/1 等枚举语义）正常加 `minimum`/`maximum`。

示例（UserCreateReq 迁移后）：

```go
type UserCreateReq struct {
    Username string `json:"username" minLength:"2" maxLength:"30"`
    Password string `json:"password" minLength:"6" maxLength:"64"`
    Nickname string `json:"nickname" minLength:"1" maxLength:"30"`
    DeptID   ID     `json:"deptId"`
    PostIDs  []ID   `json:"postIds,omitempty"`
    RoleIDs  []ID   `json:"roleIds,omitempty"`
    Email    string `json:"email,omitempty" format:"email" maxLength:"50"`
    Mobile   *string `json:"mobile,omitempty" maxLength:"20"`
    Sex      int8   `json:"sex,omitempty" minimum:"0" maximum:"2"`
    Avatar   string `json:"avatar,omitempty" maxLength:"512"`
    Remark   string `json:"remark,omitempty" maxLength:"500"`
    Status   int8   `json:"status,omitempty" minimum:"0" maximum:"1"`
}
```

**types.ID**：Body 场景已有 `MarshalJSON/UnmarshalJSON`（字符串形态）；但 huma 反射推导 schema 会标成 integer（实际是 string），且 query/path 参数需要文本解析。补两件：

```go
// UnmarshalText 支持 path/query 参数解析（huma 参数管线使用）。
func (i *ID) UnmarshalText(b []byte) error {
    v, err := strconv.ParseInt(string(b), 10, 64)
    if err != nil {
        return errs.New(errs.CodeParam, "ID 参数非法")
    }
    *i = ID(v)
    return nil
}

// Schema 实现 huma.SchemaProvider：文档标注 string（真实 JSON 形态）。
func (ID) Schema(r *huma.Registry) *huma.Schema {
    return &huma.Schema{Type: "string", Description: "雪花/段号 ID，字符串形态防 JS 精度丢失"}
}
```

> ✅ 已从 huma v2.39.1 源码验证（无需再冒烟确认）：
> - path/query 参数解析：huma 参数管线兜底支持 `encoding.TextUnmarshaler`（huma.go:1907，含官方 path 参数测试用例）
> - 文档形态：实现 `UnmarshalText` 后 schema **自动**推导为 `TypeString`（schema.go:809-815），`Schema()` 方法仅在需要自定义描述时才加，非必需

查询结构体：`form` 标签旁补 `query` 标签（gin 读 form，huma 读 query，可共存）：

```go
PageNum int `form:"page_num" query:"page_num" minimum:"1"`
```

### D4 响应：泛型 helper，成功无数据保持 `"data":null`

```go
// devops-system/internal/api/v1/wire.go
func ok[T any](data T) response.BaseResponse[T] {
    return response.BaseResponse[T]{Code: errs.CodeOK, Msg: "ok", Data: data}
}

// 无数据响应：Data 为 nil any，序列化 "data":null，与旧 OK(c, nil) 字节一致
type noData = any

func okEmpty() response.BaseResponse[any] { return ok[any](nil) }

func okPage[T any](list T, total int64) response.BaseResponse[response.PageData[T]] {
    return response.BaseResponse[response.PageData[T]]{
        Code: errs.CodeOK, Msg: "ok",
        Data: response.PageData[T]{List: list, Total: total},
    }
}
```

### D5 文档门控：`apidoc.Enabled` 只门控文档端点，不再影响路由注册

现状：Enabled=false 时 login/refresh 走 gin。迁移后**路由恒由 huma 注册**，Enabled 仅决定是否挂载 `/apidoc` UI 与 `openapi.json`：

```go
// apidoc.go 改造
type Options struct { /* ... */ Enabled bool }

func New(r gin.IRouter, opts Options) huma.API {
    opts = opts.Defaults()
    cfg := huma.DefaultConfig(opts.Title, opts.Version)
    cfg.DocsPath = opts.DocsPath
    cfg.OpenAPIPath = opts.DocsPath + "/openapi"
    if !opts.Enabled {
        cfg.OpenAPIPath = ""
        cfg.DocsPath = ""
        cfg.SchemasPath = "" // 必须一并清空：否则 /schemas/{schema} 路由仍会被注册，
                             // 且响应 $schema 链接指向不存在的路径（api.go:613 按此字段独立挂载）
    }
    // ...（安全方案、servers 不变）
    // doc.json 别名端点也包在 opts.Enabled 判断内
}
```

> ✅ 已从 huma v2.39.1 源码验证（api.go:559/609）：`OpenAPIPath`/`DocsPath` 为空时**仅跳过 HTTP 端点注册**，`api.OpenAPI()` 仍完整可程序化导出；schema 引用固定为 `#/components/schemas/`（defaults.go:56，map registry 前缀与路径无关），ref 完整性无虞。唯一注意点是 `SchemasPath` 需一并置空（见上）。

### D6 错误响应文档化：自定义 Operation.Responses

huma 为 `Operation.Errors` 生成的是 problem+json schema，与我们的信封不符。封装 helper 统一声明：

```go
var errResp = map[string]*huma.Response{
    "400": {Description: "参数错误", Content: envelopeContent()},
    "401": {Description: "未登录或会话已过期", Content: envelopeContent()},
    "403": {Description: "无操作权限", Content: envelopeContent()},
    "404": {Description: "资源不存在", Content: envelopeContent()},
    "500": {Description: "服务器内部错误", Content: envelopeContent()},
} // envelopeContent() 引用注册好的 BaseResponse[any] schema
```

### D7 公共业务组件下沉 devops-infra（复用形态对齐既有回调模式）

已核实的共享层现状（本决策的基础，均已在源码确认）：
- `security/middleware.Authenticator` 框架无关（authenticate.go:37：Bearer 解析 → token 解析 → 会话态，返回 `*securityjwt.Claims` + 类型化错误）
- 同包 `gin.go` 已示范「决策在 infra、呈现靠回调」模式：`GinOptions{OnUnauthorized/OnForbidden/OnInfrastructureError}` + `CurrentClaims`（claims 类型即 `*securityjwt.Claims`）
- `security/authz.Authorizer` 接口（Enforce/EnforcePlatform/IsPlatformAdmin）已在 infra
- `tenant.With/From` 上下文机制在 infra（contextKey 未导出，只能经 `With` 构造，见 D2）
- 业务边界转换已有先例：`auth.FromShared`（devops-system internal/auth/auth.go:45，shared claims → 业务 AccessClaims）

**下沉原则**：与 gin.go 同包同模式新增 huma 形态——infra 只产出决策（claims/租户上下文/允许与否），
信封呈现由业务经回调注入；后续业务模块（含 devops-id 若暴露 HTTP）直接复用，不再各写一套。

| 组件 | infra 落点 | 业务注入点 |
|---|---|---|
| JWT 认证 | `security/middleware/huma.go`：`HumaAuthenticate(authn, HumaOptions)`——认证后 claims 经 `huma.WithValue` 入 context（与 gin 版同一 ClaimsContextKey 语义） | `HumaOptions` 三回调写 `BaseResponse` 信封 |
| 域内鉴权 | 同文件：`HumaRequirePermission(authorizer, perm, opts)`——读 context claims → Enforce | 同上；`Authorizer` 实现由业务 authz 封装提供 |
| 平台鉴权 | 同文件：`HumaRequirePlatformPermission(authorizer, perm, opts)`——g2 校验 + dom="*" Enforce | 同上 |
| 租户注入 | `tenant/huma.go`：`HumaTenantContext(check func(ctx, tenantID) bool)`——claims → check 回调 → `huma.WithContext`+`infratenant.With` 注入（D2 已验证机制） | check 回调即业务 `tenantUsable`（DB 查询 + Redis 缓存实现留在业务侧） |
| 响应信封 | `http/response/huma.go`：`OKBody[T]`/`EmptyBody`/`PageBody[T]`/`WriteHumaEnvelope`（复用现有 `BaseResponse[T]`/`PageData[T]`） | 无（纯 helper） |
| 错误信封/文档 | `http/humaerr`（D1）+ D6 `errResp`/`envelopeContent` 一并入此包 | 无 |

后续业务模块接入 SOP：

```go
opts := securitymw.HumaOptions{OnUnauthorized: write401, OnForbidden: write403, OnInfrastructureError: write503}
biz := huma.Middlewares{
    securitymw.HumaAuthenticate(authn, opts),                          // 认证
    tenantmw.HumaTenantContext(tenantUsable),                        // 租户校验+上下文注入
    securitymw.HumaRequirePermission(authorizer, "mod:obj:act", opts), // 域内鉴权
}
huma.Register(api, with(common, huma.Operation{..., Middlewares: biz}), h.List)
```

> devops-system 的 `ClaimsFrom(ctx)` 实现为 `auth.FromShared(securitymw.HumaCurrentClaims(ctx))`——
> 业务 handler 签名与调用链不变（FromShared 已存在）。

---

## 3. 文件级改动清单

### 新增（7 个：infra 4 + system 3，见 D7）

| 文件 | 内容 |
|---|---|
| `devops-infra/http/humaerr/humaerr.go` | Envelope + `Install()` + 校验错误格式化 + D6 `errResp`/`envelopeContent`（~100 行） |
| `devops-infra/security/middleware/huma.go` | huma 版认证/鉴权（回调模式镜像同包 gin.go）：`HumaAuthenticate`/`HumaRequirePermission`/`HumaRequirePlatformPermission`/`HumaCurrentClaims`（~120 行） |
| `devops-infra/tenant/huma.go` | `HumaTenantContext(check)`：读 claims → 业务 check 回调 → `huma.WithContext`+`infratenant.With` 注入（~40 行） |
| `devops-infra/http/response/huma.go` | `OKBody[T]`/`EmptyBody`/`PageBody[T]`/`WriteHumaEnvelope` 信封 helper（~40 行） |
| `devops-system/internal/middleware/huma.go` | 装配胶水（≤60 行）：构造 `HumaOptions` 信封回调、注入 `tenantUsable`、`ClaimsFrom`（经 `auth.FromShared`） |
| `devops-system/internal/api/v1/wire.go` | 薄别名（~10 行，可选——handler 直接用 `response.OKBody` 等亦可） |
| `devops-system/internal/router/operations.go` | 40 个 `huma.Register` 按模块拆分注册函数（`registerAuth/registerUsers/...`） |

### 修改（8 个）

| 文件 | 改动 |
|---|---|
| `internal/api/v1/auth.go` | 7 个 handler 改 huma 签名 + Input/Output 类型（login/refresh 连同 `withClientIP`/`clientIP` 客户端 IP 中间件从 router.go 移入统一） |
| `internal/api/v1/user.go` | 6 个 handler 重写 |
| `internal/api/v1/role.go` | 6 个 handler 重写 |
| `internal/api/v1/menu.go` | 5 个 handler 重写 |
| `internal/api/v1/dept.go` | 5 个 handler 重写 |
| `internal/api/v1/post.go` | 5 个 handler 重写（最先做，出样板） |
| `internal/api/v1/tenant.go` | 6 个 handler 重写 |
| `internal/types/{types,user,admin,tenant,dto}.go` | ID 补 `UnmarshalText`/`Schema`；请求 DTO 标签替换；查询结构体补 `query` |
| `devops-infra/http/apidoc/apidoc.go` | `Options.Enabled`；禁用时 DocsPath/OpenAPIPath 置空 |
| `internal/router/router.go` | 重写：删全部 gin 业务注册与 `registerAuthOperations`，改为装配 + 调 operations.go；`/.well-known/jwks.json` 保留 gin 注册（基础设施路由，供 Higress jwt-auth 拉取，不入业务文档） |
| `main.go`（或 root.go） | 启动时 `humaerr.Install()` |

### 删除

- `internal/middleware/jwt.go`、`tenant.go`、`casbin.go` 中的 gin 版中间件（huma 版就位后）
- `response.Wrap` 在 devops-system 内的全部使用（handler 不再是 gin 签名；infra 包本身保留）

### 测试改造（2 个）

- `internal/router/router_test.go`：`TestRegisterAPIDoc` 增断言全部 35 条 path 存在；`TestRegisterAPIDocEnabledKeepsAuthRoutes` 保留（语义不变）；新增 openapi 基线快照测试
- `internal/middleware/middleware_test.go`：改测 huma 中间件（用 `humatest`）

---

## 4. 模块样板（posts，迁移 SOP 的复制模板）

```go
// internal/api/v1/post.go（迁移后）
package api

import (
    "context"
    "net/http"

    "github.com/danielgtaylor/huma/v2"

    "devops-infra/errs"
    "devops-system/internal/middleware"
    "devops-system/internal/service"
    "devops-system/internal/types"
)

type PostHandler struct{ svc *service.Post }

func NewPostHandler(svc *service.Post) *PostHandler { return &PostHandler{svc: svc} }

// ---- Input / Output ----

type PostListInput struct {
    Query types.PostPageQuery // struct tag `query:"" inline`
}

type PostListOutput struct {
    Body response.BaseResponse[response.PageData[[]types.PostDTO]]
}

type PostIDInput struct {
    ID types.ID `path:"id"`
}

type PostCreateInput struct {
    Body types.PostSaveReq
}

type PostEmptyOutput struct {
    Body response.BaseResponse[any]
}

// ---- Handlers ----

func (h *PostHandler) List(ctx context.Context, in *PostListInput) (*PostListOutput, error) {
    list, total, err := h.svc.List(ctx, in.Query)
    if err != nil {
        return nil, errs.ToHumaError(err)
    }
    return &PostListOutput{Body: okPage(list, total)}, nil
}

func (h *PostHandler) Get(ctx context.Context, in *PostIDInput) (*PostGetOutput, error) {
    resp, err := h.svc.Get(ctx, in.ID)
    if err != nil {
        return nil, errs.ToHumaError(err)
    }
    return &PostGetOutput{Body: ok(resp)}, nil
}

func (h *PostHandler) Create(ctx context.Context, in *PostCreateInput) (*PostEmptyOutput, error) {
    if err := h.svc.Create(ctx, middleware.ClaimsFrom(ctx), in.Body); err != nil {
        return nil, errs.ToHumaError(err)
    }
    return &PostEmptyOutput{Body: okEmpty()}, nil
}
// Update / Delete 同模式
```

注册（operations.go）：

```go
func registerPosts(api huma.API, h *apiv1.PostHandler) {
    biz := func(perm string) huma.Middlewares {
        return huma.Middlewares{middleware.JWT(jwtM), middleware.Tenant(), middleware.Perm(perm)}
    }
    common := huma.Operation{Tags: []string{"posts"}, Errors: []int{400, 401, 403, 404, 500}, Responses: errResp}

    huma.Register(api, with(common, huma.Operation{
        OperationID: "post-list", Method: http.MethodGet, Path: "/api/v1/posts",
        Summary: "岗位列表", Middlewares: biz("system:post:list"),
    }), h.List)

    huma.Register(api, with(common, huma.Operation{
        OperationID: "post-get", Method: http.MethodGet, Path: "/api/v1/posts/{id}",
        Summary: "岗位详情", Middlewares: biz("system:post:query"),
    }), h.Get)

    huma.Register(api, with(common, huma.Operation{
        OperationID: "post-create", Method: http.MethodPost, Path: "/api/v1/posts",
        Summary: "创建岗位", Middlewares: biz("system:post:create"),
    }), h.Create)
    // Update / Delete 同模式
}
```

要点：
- 路径 `:id` → `{id}`（humagin 适配器自动转回 gin 的 `:id`）
- 分组中间件收敛为 `biz(perm)` / `plat(perm)` 两个工厂，38 处不重复
- login/refresh 的 `Security: []map[string][]string{}`（公开覆盖）保留现有写法

---

## 5. 行为契约对齐表（回归验收口径）

| 场景 | 旧契约 | 新契约 | 一致性 |
|---|---|---|---|
| 成功（有数据） | 200 `{"code":200,"msg":"ok","data":{...}}` | 同 | ✅ |
| 成功（无数据） | 200 `"data":null` | 同（`BaseResponse[any]` nil） | ✅ |
| 分页 | 200 `data:{list,total}` | 同 | ✅ |
| 分页参数显式 0 值（`?page_num=0`） | 200（`omitempty` 放行，dao 兜底默认页） | 同（分页字段不加 `minimum`，dao 兜底） | ✅ |
| 业务失败 | BizError code/msg 信封 | `ToHumaError`→`NewError`→信封 | ✅ |
| 参数校验失败 | 400 信封 +「字段 X 未通过 Y 校验」 | 400 信封 +「参数校验失败: body.x: ...」（文案略变） | ⚠ 文案变化，需前端确认 |
| 非法 JSON | 400「请求体不是合法 JSON」 | 400 信封（文案略变） | ⚠ 文案变化 |
| 未登录 | 401 信封 | 中间件直写信封 | ✅ |
| 无权限 | 403 信封 | 同 | ✅ |
| 404/405 | infra NoRoute/NoMethod 信封 | 同（huma 路由也在 gin 上） | ✅ |
| Content-Type(错误) | application/json | application/json（无 ContentTypeFilter） | ✅ |
| ID 字符串形态 | 字符串进出 | 同 + 文档修正为 string | ✅ |

---

## 6. 分阶段实施（每阶段一个 PR，原子可回滚）

> gin 与 huma 不能同路径共存（gin 重复注册 panic），**不能灰度共存**；故按模块整块切换，模块内一次原子替换。

### P0 基础设施（零行为变更，现有测试全绿；infra 优先，见 D7）
1. `devops-infra/http/humaerr`：Envelope + `Install()` + D6 `errResp`/`envelopeContent`；main 启动 `Install()`
2. `devops-infra/security/middleware/huma.go`：huma 版认证/鉴权中间件（回调模式同包 gin.go）
3. `devops-infra/tenant/huma.go`：`HumaTenantContext`（claims → check 回调 → `huma.WithContext`+`infratenant.With`）
4. `devops-infra/http/response/huma.go`：`OKBody`/`EmptyBody`/`PageBody`/`WriteHumaEnvelope`
5. devops-system `middleware/huma.go` 装配胶水：HumaOptions 信封回调 + tenantUsable 注入 + `ClaimsFrom`
6. `apidoc.Options.Enabled`
7. `types.ID` 补 `UnmarshalText`/`Schema`
8. **humatest 冒烟**（回归确认）：ID path/query 解析、信封错误 400、disabled 时 `/apidoc` 与 `/schemas` 均 404 且 `doc.json` 仍可程序化导出（两点技术风险已从源码关闭，见 D3/D5 ✅）
9. types 请求 DTO 标签替换（含查询结构体 query 标签、`*int8` 指针化）

验收：`go build ./... && go test ./...` 全绿；行为无任何变化（老 gin 路由仍在跑）。

### P1 auth 模块（7 接口）
login/refresh 移入 operations.go 统一注册（删 router.go 里的重复逻辑、`withClientIP`/`clientIP` 辅助函数与 `if !docCfg.Enabled` 分支，login 的 ClientIP 中间件随迁）；logout/info/routers/profile/password 迁 huma。
验收：契约测试 + openapi paths 含 7 条 auth；手工 smoke 登录→info→改密→登出。

### P2 posts 模块（5 接口，样板）
端到端跑通后产出本文档附录《模块迁移 SOP》（Input/Output→注册→测试三步复制模板）。

### P3 roles(6) + depts(5) + menus(5)
### P4 users(6)
### P5 tenants(6，平台路由用 `plat(perm)` 工厂)
### P6 清理与契约固化（终态：零双轨）
- 删 devops-system 全部 gin 业务面代码：gin 版中间件（jwt.go/tenant.go/casbin.go）、`response.Wrap` 调用、`registerAuthOperations`、wire 残留
- 清理 devops-infra 无消费方的 gin 中间件变体：`security/middleware/gin.go`/`gin_test.go` 与 infra README 对应章节——已核实当前全仓仅 README/测试自引用、无业务消费方，grep 复核后删除
- openapi.json 基线入库（testdata/golden），CI 增加 diff 门禁
- README/docs 更新（本文档状态改为已完成；infra README 中间件章节改为 huma 形态示例）

---

## 7. 测试与契约保护

1. **行为测试**（humatest）：每模块一文件，覆盖 happy path / 400 信封（断言 code/msg/data 三字段）/ 401（无 token）/ 403（越权 perm）。humatest 属 huma 模块自带，无需新依赖。
2. **契约快照**：`testdata/openapi.golden.json` + 比对测试。P2 完成后生成基线；后续每模块迁移审查 diff（paths/schema 字段级）。
3. **既有测试改造**：`router_test.go`（断言信封与全量 paths）、`middleware_test.go`（改 humatest）。
4. **手工 smoke 清单**（每阶段）：登录 → 列表分页 → 详情 404 → 越权 403 → 无 token 401 → 停用租户 401 → openapi.json/UI 人工过目。

---

## 8. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| 校验失败文案变化 | 前端若解析 msg 展示 | 文案在 `formatValidation` 中尽量对齐「参数校验失败: …」前缀；提前告知前端 |
| `OpenAPIPath=""` 时 ref 破损 | 前端代码生成消费 doc.json | ✅ 已源码验证不存在（api.go:559 仅门控端点注册，refs 固定 `#/components/schemas/`）；注意 `SchemasPath` 一并置空 |
| types.ID 参数解析不受支持 | path/query 返回 400 | ✅ 已源码验证不存在（huma.go:1907 TextUnmarshaler 兜底；schema 自动 string） |
| required 语义差异（omitempty 误标） | 文档必填性失真 / 前端漏传被 400 | 标签替换走映射表 + code review 逐字段核对 |
| 数字 min/max（`minimum:"0"`）行为差异 | 校验松紧变化 | humatest 用例覆盖边界值 0/1/2 |
| 租户上下文传播方式错误（自造 key / 漏传 / 误用 WithValue 塞租户值） | **租户过滤静默失效，跨租户数据泄露** | D2 已写明：必须 `huma.WithContext` + `infratenant.With` 整体替换底层 ctx；middleware_test 复刻 gin 版全部租户用例并断言 dao 层行级过滤生效 |
| 分页参数显式 0 值被 400（误加 `minimum`） | 前端传 `page_num=0` 的调用失败 | D3 决策：分页字段不加 `minimum`，0 值交 dao 兜底；humatest 覆盖 0 值边界用例 |
| 双重校验语义（validator → huma）遗漏某约束 | 约束丢失 | 映射表逐条核对，快照 diff 审查 schema |
| 中间件移植遗漏（如租户缓存 key） | 权限/租户旁路 | huma 中间件测试复刻 gin 版全部用例（middleware_test 现有用例语义） |

---

## 9. 明确不做（scope 外）

- 不替换 gin 为 stdlib/chi：httpinfra.Run、Recovery/AccessLog、healthz/readyz/metrics、JWKS、NoRoute/NoMethod 全保留
- 不动 service/dao/model 层、多租户链路、Leaf ID、Casbin 模型
- 不删 `response` 包（devops-infra 共享库，其他服务仍在用）——且按 D7 **新增** huma 信封 helper（OKBody/EmptyBody/PageBody）
- devops-id 服务不在本期范围（可复用本方案的 P0 基建）

## 10. 工作量速览

- 新增 7 文件（infra 4：humaerr / security-middleware-huma / tenant-huma / response-huma；system 3：装配胶水 / wire 薄别名 / operations.go）
- 修改 10 文件（7 handler + types 域 + apidoc + router + main）
- 删除：devops-system gin 业务面（中间件 3 处 + Wrap 调用）+ infra 无消费方 gin 中间件变体（P6 grep 复核后）
- 40 个接口 = 40 个 huma.Register（posts 样板复制）

---

## 附录：模块迁移 SOP（P2 后固化）

1. **Input/Output**：按 D3 映射表为该模块 DTO 复核标签；handler 文件内定义 Input/Output 结构体（复用 service 现有 DTO 做 Body）
2. **Handler**：签名 `func(ctx context.Context, in *XxxInput) (*XxxOutput, error)`；`errs.ToHumaError` 收口错误；claims 用 `middleware.ClaimsFrom(ctx)`
3. **注册**：operations.go 加 `registerXxx(api, h)`，业务路由 `biz(perm)`、平台路由 `plat(perm)`
4. **删旧**：router.go 对应 gin 注册块整块删除（防重复注册 panic）
5. **测试**：humatest 行为用例 + openapi 快照 diff 审查
6. **验收**：`go build ./... && go test ./...` + 手工 smoke 清单
