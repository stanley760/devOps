# devops-infra 增强中间件使用指南

`devops-infra` 是 devOps 平台的共享基础设施模块。本文面向**业务服务开发者**，详细说明如何调用本模块中已经增强的中间件能力，包括：

| 能力 | 包路径 | 说明 |
| --- | --- | --- |
| HTTP 通用中间件 | `devops-infra/http/middleware` | panic 恢复、访问日志，`http.Run` 开箱即用 |
| gRPC 通用拦截器 | `devops-infra/grpc/interceptor` | panic 恢复、访问日志，`grpc.Run` 开箱即用 |
| JWT 认证 | `devops-infra/security/jwt` | RS256 + kid 签发/验签、JWKS 输出 |
| Redis 会话存储 | `devops-infra/security/session` | 刷新会话态（踢下线、轮换、批量撤销） |
| 认证 / 鉴权中间件 | `devops-infra/security/middleware` | 框架无关认证器 + Gin 中间件三件套 |
| Casbin 权限决策 | `devops-infra/security/authz` | 多租户域模型 RBAC，gorm-adapter 持久化 |
| 多租户中间件 | `devops-infra/tenant` | 请求头注入租户上下文 + GORM 行级隔离 |
| 统一响应与错误 | `devops-infra/http/response`、`devops-infra/errs` | 统一信封、统一错误码 |

> 本模块只做**决策**，不做**呈现**：中间件产出 claims / 租户上下文 / 允许与否的判定；HTTP 状态码文案、响应信封由各业务服务通过回调（`GinOptions`）或 `response` 包自行定制。

---

## 目录

1. [整体架构：请求经过的中间件链](#1-整体架构请求经过的中间件链)
2. [快速开始](#2-快速开始)
3. [HTTP 通用中间件](#3-http-通用中间件)
4. [gRPC 通用拦截器](#4-grpc-通用拦截器)
5. [统一响应与错误处理](#5-统一响应与错误处理)
6. [JWT 认证：签发、验签与会话](#6-jwt-认证签发验签与会话)
7. [认证 / 鉴权 Gin 中间件](#7-认证--鉴权-gin-中间件)
8. [Casbin 权限决策与策略管理](#8-casbin-权限决策与策略管理)
9. [多租户中间件与数据隔离](#9-多租户中间件与数据隔离)
10. [中间件编排最佳实践](#10-中间件编排最佳实践)
11. [单元测试支持](#11-单元测试支持)
12. [附录：API 速查表](#12-附录api-速查表)

---

## 1. 整体架构

一个受保护的 HTTP 请求按以下顺序穿过中间件链（gRPC 同理）：

```
客户端
  │  Authorization: Bearer <access_token>
  │  X-Tenant-ID: <tenant_id>
  ▼
┌─────────────────────────────────────────────────────────────┐
│ http.Run 自动挂载：gin.Logger → Recovery → AccessLog        │
│   404/405 统一信封、/healthz、/readyz                        │
├─────────────────────────────────────────────────────────────┤
│ 业务路由组（按序注册）：                                     │
│   ① GinAuthenticate          Bearer → RS256 验签 → 会话态   │
│   ② tenant.Middleware        X-Tenant-ID → ctx 注入         │
│   ③ GinRequirePermission     Casbin 域内鉴权（可选）         │
│   ④ GinRequirePlatformPermission  平台管理员鉴权（可选）     │
│   ⑤ response.Wrap(handler)   统一错误出口                    │
├─────────────────────────────────────────────────────────────┤
│ 数据访问层：tenant.RegisterPlugin 自动追加 WHERE tenant_id    │
└─────────────────────────────────────────────────────────────┘
```

三条核心设计原则：

- **fail-closed**：认证器、会话存储、Casbin、租户插件任何一处内部错误都按"拒绝"处理（503 / 查询报错），绝不放行。
- **决策与呈现分离**：安全决策在 `devops-infra/security/*`，如何响应（文案、信封）由业务侧回调决定。
- **框架无关内核**：认证器 `Authenticator` 只依赖 `context` 与两个接口（`TokenParser`、`SessionStore`），Gin 只是薄适配层，可按同样方式扩展到其他框架。

---

## 2. 快速开始

以一个最小业务服务为例，5 步接入全部中间件能力：

```go
package main

import (
    "crypto/rsa"
    "log"
    "time"

    infracfg "devops-infra/config"
    httpinfra "devops-infra/http"
    securityjwt "devops-infra/security/jwt"
    securitymw "devops-infra/security/middleware"
    securitysession "devops-infra/security/session"
    "devops-infra/tenant"
)

func main() {
    // 配置经结构体映射加载（各服务自定义根 Config 组合 infracfg.ServerConfig 等）
    var cfg struct {
        Server infracfg.ServerConfig `mapstructure:"server"`
        JWT    struct {
            SigningKey    string        `mapstructure:"signing_key"`
            KeyID         string        `mapstructure:"key_id"`
            Issuer        string        `mapstructure:"issuer"`
            AccessExpire  time.Duration `mapstructure:"access_expire"`
            RefreshExpire time.Duration `mapstructure:"refresh_expire"`
        } `mapstructure:"jwt"`
    }
    if err := infracfg.LoadInto(&cfg); err != nil {
        log.Fatal(err)
    }

    // ① 构造 JWT 管理器（RS256 私钥 + 自验签 KeySet）
    privPEM := []byte(cfg.JWT.SigningKey)
    pub, err := securityjwt.PublicKeyFromPrivatePEM(privPEM)
    if err != nil {
        log.Fatal(err)
    }
    keySet, err := securityjwt.NewStaticKeySet(map[string]*rsa.PublicKey{cfg.JWT.KeyID: pub})
    if err != nil {
        log.Fatal(err)
    }
    tokenMgr, err := securityjwt.NewManager(securityjwt.Config{
        PrivateKeyPEM: privPEM,
        KeyID:         cfg.JWT.KeyID,
        Issuer:        cfg.JWT.Issuer,
        AccessExpire:  cfg.JWT.AccessExpire,
        RefreshExpire: cfg.JWT.RefreshExpire,
        KeySet:        keySet,
    })
    if err != nil {
        log.Fatal(err)
    }

    // ② 构造 Redis 会话存储与认证器
    // sessions := securitysession.NewRedisClientStore(redisClient, cfg.JWT.RefreshExpire)
    // authn := securitymw.NewAuthenticator(tokenMgr, sessions)

    // ③ 注册 GORM 租户隔离插件（可选）
    // tenant.RegisterPlugin(gormDB)

    // ④ 组装路由：认证 → 租户 → 权限 → handler（详见第 7、9 节）
    _ = securitymw.GinAuthenticate
    _ = tenant.Middleware(true)

    // ⑤ 一行启动：自动挂载 Recovery/AccessLog/healthz/readyz
    // httpinfra.Run(cfg.Server, func(r *gin.Engine) { /* 注册路由 */ })
}
```

以下各节逐项展开。

---

## 3. HTTP 通用中间件

### 3.1 `http.Run`：开箱即用的服务骨架

```go
func Run(cfg config.ServerConfig, register Register, readiness ...ReadinessCheck) error
```

`http.Run` 内部已自动完成以下装配，**业务代码无需重复挂载**：

| 内置能力 | 行为 |
| --- | --- |
| `gin.Logger()` | gin 标准请求日志 |
| `middleware.Recovery()` | panic → 统一 500 信封（见下） |
| `middleware.AccessLog()` | 请求完成后的结构化访问日志（Debug 级） |
| `SetTrustedProxies` | 按 `server.trusted_proxies` 配置可信代理，保证 `ClientIP` 正确 |
| `NoRoute / NoMethod` | 404 / 405 返回统一信封而非 gin 默认纯文本 |
| `GET /healthz` | 存活探针，返回 `{"status":"ok"}` |
| `GET /readyz` | 就绪探针，逐项执行 readiness 检查（单项 2s 超时），未就绪返回 503 并列出未就绪项 |
| 优雅退出 | 监听 SIGINT/SIGTERM，10s 排空在途请求 |

业务服务只需提供路由注册回调与就绪检查：

```go
readiness := []httpinfra.ReadinessCheck{
    {Name: "mysql", Check: mysqlClient.Ping},
    {Name: "redis", Check: redisPing},
}
err := httpinfra.Run(cfg.Server, router.Register(...), readiness...)
```

`config.ServerConfig` 对应 YAML 配置：

```yaml
server:
  port: 8080
  read_timeout: 10s
  write_timeout: 10s
  trusted_proxies: ["10.0.0.0/8"]   # 网关/SLB 网段
```

### 3.2 `Recovery()`：panic 统一处理

```go
middleware.Recovery()
```

- 捕获 handler panic，记录 `slog.Error`（含 panic 值、方法、路径、完整堆栈）；
- 响应未写出时返回统一信封 `{"code":500,"msg":"服务器内部错误"}`；
- 响应已部分写出时仅置 500（此时无法再改写为信封）。

> 刻意不用 `gin.Recovery()`——其默认输出纯文本，与统一响应格式冲突。

### 3.3 `AccessLog()`：访问日志

```go
middleware.AccessLog()
```

请求完成后以 `slog.Debug` 记录：method、path（优先路由模板 `FullPath()`）、status、latency、bytes、client_ip。生产环境将日志级别调到 `info` 及以上即自动静默，需要排查时调回 `debug`，无需改代码。

---

## 4. gRPC 通用拦截器

### 4.1 `grpc.Run`：服务端骨架

```go
grpcinfra.Run(grpcinfra.ServerConfig{
    Listen:     ":9090",
    Reflection: cfg.Env == "dev", // dev 开启 reflection，便于 grpcurl 调试
}, func(s *grpc.Server, hc *health.Server) {
    pb.RegisterXxxServer(s, svc)
    hc.SetServingStatus("xxx.Service", healthpb.HealthCheckResponse_SERVING)
})
```

自动挂载：

- `interceptor.Recovery()`：panic → `codes.Internal`（"服务器内部错误"），堆栈只进日志，不向客户端泄露细节；
- `interceptor.AccessLog()`：每个一元调用完成后以 `slog.Debug` 记录 method、status code、latency；
- `grpc.health.v1` 标准健康检查服务；
- SIGINT/SIGTERM 优雅退出：`GracefulStop` 最多等 10s，超时强停。

### 4.2 客户端拨号

```go
conn, err := grpcinfra.Dial(ctx, "devops-id:9090", 5*time.Second)
defer grpcinfra.Close(conn)

// 判断连接类错误，供上层决定是否重试
if grpcinfra.IsUnavailable(err) { ... }
```

`Dial` 返回非阻塞就绪连接，内置 `Unavailable` 指数退避重试——对发号类幂等调用安全（重试至多烧号，绝不重号）。

---

## 5. 统一响应与错误处理

中间件与业务 handler 共用同一套响应约定。所有接口返回信封：

```json
{"code": 200, "msg": "ok", "data": {...}}
```

`code` 与 HTTP 状态码同空间（200/400/401/403/404/405/500）。

### 5.1 handler 写法：`response.Wrap`

业务 handler 以**返回 error** 的方式报告失败，成功路径自行写响应：

```go
func (h *UserHandler) Get(c *gin.Context) error {
    id, err := strconv.ParseInt(c.Param("id"), 10, 64)
    if err != nil {
        return errs.New(errs.CodeParam, "用户 ID 非法")        // → 400
    }
    user, err := h.svc.Get(c.Request.Context(), id)
    if err != nil {
        return err                                            // → 按错误类型分流
    }
    response.OK(c, user)                                      // → 200 信封
    return nil
}

// 注册：Wrap 负责把 error 转成响应
r.GET("/api/v1/users/:id", response.Wrap(userH.Get))
```

`Wrap` 的分流规则：

| handler 返回 | 响应 |
| --- | --- |
| `nil` 且已写响应 | 原样输出 |
| `nil` 但未写响应 | 500（兜底，记日志，防请求挂起） |
| `*errs.BizError`（含 `%w` 包装） | 按其 `Code`/`Msg` 输出 |
| JSON 绑定/校验错误（`ShouldBindJSON` 直返） | 400，带字段级中文提示 |
| 其他未知错误 | 500，细节只进日志 |

### 5.2 错误定义：`errs`

```go
// 直接使用预定义错误
return errs.ErrNotFound                       // 404 资源不存在

// 附加业务语义并保留底层原因（日志可见，客户端不可见）
return errs.Wrap(err, errs.CodeInternal, "签发 token 失败")
```

错误码常量：`CodeOK(200)` `CodeParam(400)` `CodeUnauthorized(401)` `CodeForbidden(403)` `CodeNotFound(404)` `CodeMethodNotAllowed(405)` `CodeInternal(500)`。

### 5.3 常用响应出口

| 函数 | 用途 |
| --- | --- |
| `response.OK(c, data)` | 200 成功，`data` 传 `nil` 表示无数据 |
| `response.OKPage(c, list, total)` | 200 分页：`data: {list, total}` |
| `response.Fail(c, code, msg)` | 失败出口，仅放行 4xx/5xx，`AbortWithStatusJSON` |
| `response.FailErr(c, err)` | 错误统一出口（`Wrap` 内部使用，也可直接调用） |

---

## 6. JWT 认证

包路径 `devops-infra/security/jwt`。采用 **RS256 非对称签名 + kid 密钥标识**：签发方持私钥，验签方只需公钥（甚至只需 JWKS 端点），天然支持网关验签与密钥轮换。

### 6.1 构造签名管理器

```go
// 私钥 PEM（PKCS#1 或 PKCS#8 均可），通常来自配置项 jwt.signing_key
privPEM := []byte(cfg.JWT.SigningKey)

// 单签发方部署：用自身私钥导出公钥，组装自验签 KeySet
pub, err := securityjwt.PublicKeyFromPrivatePEM(privPEM)
if err != nil { log.Fatal(err) }

keySet, err := securityjwt.NewStaticKeySet(map[string]*rsa.PublicKey{
    "devops-system-rs256-1": pub,   // kid → 公钥；轮换期可同时放新旧两把
})
if err != nil { log.Fatal(err) }

tokenMgr, err := securityjwt.NewManager(securityjwt.Config{
    PrivateKeyPEM: privPEM,          // 签发用私钥
    KeyID:         "devops-system-rs256-1",  // 写入 token header 的 kid
    Issuer:        "devops-system",  // iss，验签时强制校验
    AccessExpire:  30 * time.Minute, // access token 有效期
    RefreshExpire: 7 * 24 * time.Hour, // 刷新会话有效期（session.TTL 默认值）
    KeySet:        keySet,           // 验签用公钥集合
})
```

`Config.Validate` 要求所有字段非空，缺失时返回 `ErrInvalidConfig`。仅需**验签**的服务可以不配 `PrivateKeyPEM`（不签发），但 `KeySet` 必须提供。

### 6.2 签发 access token

```go
pair, err := tokenMgr.Issue(ctx, securityjwt.Subject{
    UserID:     42,
    Username:   "alice",
    TenantID:   7,
    TenantCode: "tenant-7",
}, sessionID)   // sessionID 由调用方生成（推荐 uuid），同时作为刷新会话的 sid
```

返回 `TokenPair{AccessToken, SessionID, AccessExpiresIn}`。claims 载荷结构：

| 字段 | JSON 键 | 含义 |
| --- | --- | --- |
| `UID` | `uid` | 用户 ID |
| `Username` | `username` | 用户名 |
| `SessionID` | `sid` | 会话 ID（刷新凭据） |
| `TenantID` | `tid` | 租户 ID |
| `TenantCode` | `tcode` | 租户编码 |
| 标准字段 | `iss/sub/iat/nbf/exp` | `sub` 为用户 ID 字符串 |

### 6.3 解析验签

```go
claims, err := tokenMgr.ParseAccess(tokenString)
// 成功：*Claims；失败：ErrInvalidConfig（配置问题）或 ErrInvalidToken（伪造/过期/kid 未知/issuer 不符）
```

强制校验：RS256 算法、header `kid` 存在且能在 `KeySet` 中找到、`iss` 匹配、`exp` 必须存在且未过期。

### 6.4 JWKS 端点（网关验签对接）

```go
// 暴露 RFC 7517 JWKS 文档，供 Higress jwt-auth 插件（jwksFromUrl）或其他服务拉取公钥
r.GET("/.well-known/jwks.json", func(c *gin.Context) {
    doc, err := tokenMgr.JWKS()   // {"keys":[{"kty":"RSA","kid":"...","use":"sig","alg":"RS256","n":"...","e":"..."}]}
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "jwks unavailable"})
        return
    }
    c.JSON(http.StatusOK, doc)
})
```

仅输出公钥材料（n/e），私钥永不离开签发方。轮换密钥时：新 token 用新 `kid` 签发，`KeySet` 同时保留新旧公钥，旧 token 在 `exp` 内仍可验签，实现平滑过渡。

### 6.5 Redis 会话存储（`security/session`）

JWT 一旦签发无法主动作废，因此每个 access token 绑定一个服务端会话（`sid`）。**每个请求都校验会话存在**——登出、改密、停用用户后，旧 token 立即失效。

```go
// 两种构造方式：包装 cache/redis 的 Client，或直接传 go-redis 客户端
sessions := securitysession.NewRedisStore(cacheRedisClient, 7*24*time.Hour)
sessions := securitysession.NewRedisClientStore(goRedisClient, 7*24*time.Hour)
```

`Store` 接口全量方法：

```go
type Store interface {
    Create(ctx, Session) error                    // 建会话（登录/刷新时）
    Active(ctx, SessionRef) (bool, error)         // 会话是否有效（认证中间件每请求调用）
    Lookup(ctx, sid) (tenantID, userID, ok)       // 由 sid 反查归属（refresh 端点用）
    Matches(ctx, SessionRef, userID) (bool, error)// 校验 sid 确属该用户
    Revoke(ctx, SessionRef) error                 // 撤销单个会话（登出/轮换）
    RevokeUser(ctx, tenantID, userID) error       // 撤销用户全部会话（改密/停用 → 踢下线）
    RevokeTenant(ctx, tenantID) error             // 撤销租户全部会话（停用租户 → 全员下线）
}
```

Redis 键空间（**按租户分段**，便于按租户清理与审计）：

| 键 | 值 | TTL |
| --- | --- | --- |
| `t{tid}:auth:refresh:{sid}` | `user_id` | 会话 TTL |
| `t{tid}:auth:sids:{uid}` | SET\<sid\> | 永久（供批量撤销索引） |
| `auth:sid2uid:{sid}` | `"tid:uid"` | 会话 TTL |

登录/刷新的标准用法（参考 devops-system 的 `auth.Manager`）：

```go
// 登录：签发 + 建会话
sid := uuid.NewString()
pair, _ := tokenMgr.Issue(ctx, subject, sid)
_ = sessions.Create(ctx, securitysession.Session{
    TenantID: 7, UserID: 42, SessionID: sid, TTL: cfg.RefreshExpire,
})

// 刷新轮换（对齐 rotation 最佳实践）：先建新会话，再撤销旧 sid
// 登出：sessions.Revoke(ctx, SessionRef{TenantID: 7, UserID: 42, SessionID: sid})
```

---

## 7. 认证 / 鉴权 Gin 中间件

包路径 `devops-infra/security/middleware`。三个 Gin 中间件 + 一个框架无关认证器。

### 7.1 `Authenticator`：框架无关认证决策

```go
type TokenParser interface{ ParseAccess(token string) (*securityjwt.Claims, error) }
type SessionStore interface{ Active(ctx, SessionRef) (bool, error) }

authn := securitymw.NewAuthenticator(tokenMgr, sessions) // 两个依赖正好由第 6 节组件实现
```

`Authenticate(ctx, bearerHeader)` 执行三步：

1. **Bearer 解析**：要求 `Authorization: Bearer <token>` 前缀，否则 `ErrUnauthorized`；
2. **token 验签**：`ParseAccess` 失败 → `ErrUnauthorized`；配置错误 → `ErrInfrastructure`；
3. **会话态校验**：Redis `Active` 查询出错 → `ErrInfrastructure`；会话不存在 → `ErrSessionInactive`。

错误分类（用 `errors.Is` 判定）：

| 错误 | 含义 | 建议映射 |
| --- | --- | --- |
| `ErrUnauthorized` | 缺失头 / scheme 错 / token 非法或过期 | 401 |
| `ErrSessionInactive` | token 有效但服务端会话已撤销（被踢下线） | 401（提示重新登录） |
| `ErrInfrastructure` | Redis 故障、JWT 配置错误等内部问题 | 503（fail-closed） |

非 Gin 场景可直接调用 `authn.Authenticate`（定时任务间调用、其他框架适配）。

### 7.2 `GinAuthenticate`：认证中间件

```go
var opts = securitymw.GinOptions{
    OnUnauthorized: func(c *gin.Context, err error) {
        response.Fail(c, http.StatusUnauthorized, "未登录或会话已过期")
    },
    OnForbidden: func(c *gin.Context, err error) {
        response.Fail(c, http.StatusForbidden, "无操作权限")
    },
    OnInfrastructureError: func(c *gin.Context, err error) {
        slog.Error("认证基础设施错误", "err", err, "path", c.FullPath())
        response.Fail(c, http.StatusServiceUnavailable, "服务暂不可用，请稍后重试")
    },
}

// logged 组内所有路由都需要登录
logged := api.Group("", securitymw.GinAuthenticate(authn, opts))
```

认证成功后把 `*securityjwt.Claims` 注入 `gin.Context`（键 `security.claims`）并放行；失败按错误分类调用对应回调并 `Abort()`。

`GinOptions` 三个回调均可省略，默认行为是裸状态码（401/403/503 无 body）。**生产建议**像上例一样接 `response.Fail`，保持全站信封一致。

在后续 handler / 中间件中取当前用户身份：

```go
claims := securitymw.CurrentClaims(c)   // *securityjwt.Claims，未认证时为 nil
c.JSON(200, gin.H{"uid": claims.UID, "tenant": claims.TenantID})
```

> 业务服务通常会在自己的 claims 类型与共享 `Claims` 之间做一次边界转换（`FromShared`），使业务代码不直接依赖基础设施包——参考 devops-system `internal/auth`。

### 7.3 `GinRequirePermission`：租户域内鉴权

```go
users := biz.Group("/users")
users.GET("", securitymw.GinRequirePermission(authz, "system:user:list", opts), handler)
users.POST("", securitymw.GinRequirePermission(authz, "system:user:create", opts), handler)
```

内部逻辑：从 context 取 claims → 以 `claims.UID`（sub）、`claims.TenantID`（dom）调用 `authorizer.Enforce`：

- 放行 → `c.Next()`；
- Casbin 出错 → `OnInfrastructureError`（503，fail-closed）；
- 无权限 → `OnForbidden`（403）；
- claims 缺失（未先过 `GinAuthenticate`）→ `OnUnauthorized`。

### 7.4 `GinRequirePlatformPermission`：平台管理员鉴权

```go
plat := logged.Group("/tenants")   // 平台路由不挂 tenant 中间件，操作全局表
plat.GET("", securitymw.GinRequirePlatformPermission(authz, "platform:tenant:list", opts), handler)
```

双重校验，比域内鉴权多一步身份门槛：

1. `IsPlatformAdmin(uid)`：校验 `g2` 平台角色；
2. `EnforcePlatform(uid, perm)`：以 `dom="*"` 执行策略。

**防越权要点**：`platform:tenant:*` 这类平台权限标识只应出现在平台路由，不要进入租户菜单/角色表——否则持有 `p, super_admin, {tid}, *` 的租户超管会因 `p.dom == "*"` 通配而触达平台接口。

### 7.5 认证与鉴权的时序要求

`GinRequirePermission` 依赖 `CurrentClaims`，**必须注册在 `GinAuthenticate` 之后**（gin 按注册顺序执行，权限中间件必须先于 handler）：

```go
// ✔ 正确
group.GET("/x", GinAuthenticate(authn, opts), GinRequirePermission(authz, "p", opts), handler)
// ✘ 错误：权限中间件拿不到 claims，全部 401
```

---

## 8. Casbin 权限决策与策略管理

包路径 `devops-infra/security/authz`。基于 Casbin + gorm-adapter（策略持久化在 `casbin_rule` 表），实现**多租户域模型 RBAC**。

### 8.1 初始化

```go
authorizer, err := securityauthz.New(gormDB)   // 自动建表、加载存量策略
if err != nil { log.Fatal(err) }
// authorizer 同时满足 middleware 中用到的 Authorizer 接口，
// 直接传给 GinRequirePermission / GinRequirePlatformPermission
```

### 8.2 域模型

```
[request_definition]  r = sub, dom, obj        sub=用户ID  dom=租户ID  obj=权限标识
[policy_definition]   p = sub, dom, obj
[role_definition]     g = _, _, _              g(用户, 角色, 租户)：租户内角色三元组
                      g2 = _, _                g2(用户, platform_admin)：平台角色（不绑域）
[matchers]            m = (g(r.sub, p.sub, r.dom) || g2(r.sub, p.sub))
                          && (r.dom == p.dom || p.dom == "*") && keyMatch(r.obj, p.obj)
```

- **g 三元组**：同一 `user_id` 在不同租户是不同的 g 行，角色归属天然按租户隔离；
- **g2 二元组**：平台管理员不绑域，配合 `p, platform_admin, *, *` 跨域全通；
- `keyMatch`：权限标识支持 `*` 通配，如 `p, super_admin, 7, system:*` 匹配该租户全部 system 权限。

### 8.3 决策 API

```go
ok, err := authorizer.Enforce(ctx, "42", "7", "system:user:create")   // 域内
ok, err := authorizer.EnforcePlatform(ctx, "42", "platform:tenant:list") // dom="*"
ok, err := authorizer.IsPlatformAdmin(ctx, "42")                      // g2 判定
```

### 8.4 策略维护（角色/授权管理接口的后端）

策略采用**快照重建**语义——先删旧行再批量加新行，避免增量同步漂移：

```go
// 保存角色权限：按 (role, tenant) 重建 p 行；perms 为空 = 清空该角色全部授权
_ = authorizer.RebuildRolePolicies("manager", "7", []string{"system:user:list", "system:user:query"})

// 删除角色：清其 p 行
_ = authorizer.RemoveRolePolicies("manager", "7")

// 保存用户角色：按 (user, tenant) 重建 g 行
_ = authorizer.RebuildUserGrouping("42", "7", []string{"manager", "auditor"})

// 删除用户在某租户的角色 / 删除角色在某租户的用户绑定
_ = authorizer.RemoveUserGrouping("42", "7")
_ = authorizer.RemoveRoleGrouping("manager", "7")

// 幂等加单条策略（种子数据初始化用）
_ = authorizer.AddPolicyIfAbsent("p", "super_admin", "7", "*")     // p 行
_ = authorizer.AddPolicyIfAbsent("g2", "1", "platform_admin")      // g2 行
```

> 建议在业务包上再封一层门面（数字 ID ↔ 字符串转换 + fail-closed 包装），handler 只面向业务语义——参考 devops-system `internal/authz`。

---

## 9. 多租户中间件与数据隔离

包路径 `devops-infra/tenant`。三层能力：请求头注入上下文 → GORM 自动行级隔离 → 受控豁免。

### 9.1 租户上下文

```go
// 注入（中间件已做，业务代码在跨 goroutine 传递 ctx 时自动携带）
ctx = tenant.WithTenant(ctx, 7)          // 仅租户 ID；ID ≤ 0 返回 ErrInvalidTenantID
ctx = tenant.With(ctx, 7, "tenant-7")    // ID + 编码

// 读取
tid, ok := tenant.TenantID(ctx)          // (7, true)
tenant, ok := tenant.From(ctx)           // Context{ID:7, Code:"tenant-7"}
tid, err := tenant.RequiredTenantID(ctx) // 未设置 → ErrTenantNotFound
code, err := tenant.RequiredTenantCode(ctx) // 未设置或编码为空 → ErrTenantNotFound
```

### 9.2 Gin 租户中间件

```go
// 默认读 "X-Tenant-ID" 请求头
biz := logged.Group("", tenant.Middleware(true))

// 自定义请求头
group := r.Group("", tenant.MiddlewareWithHeader("X-Org-ID", false))
```

`required` 参数语义：

| 值 | 头缺失 | 头非法（非正整数） |
| --- | --- | --- |
| `true` | **400**「缺少租户编号」直接拦截 | **400**「租户编号必须是正整数」 |
| `false` | 放行（无租户上下文继续） | 400 |

> 即使 `required=false` 放行了无租户请求，后续所有受 GORM 插件保护的查询仍会因缺租户上下文而**报错**（fail-closed，见下）。`false` 适用于混合路由组（部分接口公开、部分接口用租户）。

合法租户头会转换为 `tenant.WithTenant` 上下文并替换 `c.Request`，下游 handler / GORM 均可读取。

### 9.3 GORM 自动租户隔离插件

```go
import "devops-infra/tenant"

if err := tenant.RegisterPlugin(gormDB); err != nil {   // 装配期调用一次
    log.Fatal(err)
}
```

注册后，对**含 `tenant_id` 字段的模型**自动生效，业务代码零改动：

| 操作 | 注入行为 |
| --- | --- |
| `Create` | 自动填充 `tenant_id`（结构体该字段为零值时；显式赋值优先，批量创建同样支持） |
| `Query` | 自动追加 `WHERE tenant_id = ?` |
| `Update` | 自动追加 `WHERE tenant_id = ?` |
| `Delete` | 自动追加 `WHERE tenant_id = ?` |

fail-closed 保障：

- 查询/更新/删除时上下文没有租户 → 语句返回 `ErrContextMissing`，**查询根本不会执行**，杜绝裸查跨租户数据；
- 模型没有 `tenant_id` 字段（如全局配置表）→ 不做任何注入，照常执行；
- 带 `Joins` 的查询暂不自动过滤（join 语义复杂，需自行用 `Scope` 显式处理）。

```go
// handler / service 层：直接透传 gin 请求的 ctx 即可
func (s *UserSvc) List(ctx context.Context) ([]User, error) {
    var users []User
    err := s.db.WithContext(ctx).Where("status = ?", 1).Find(&users).Error
    // 实际 SQL: SELECT * FROM users WHERE status = 1 AND tenant_id = 7
    return users, err
}
```

### 9.4 手动过滤与受控豁免

不走 GORM 回调的场景（原生 SQL、跨表 join、测试）可用 `Scope` 手动追加租户条件：

```go
db := tenant.Scope(gormDB, ctx, "")        // 列名缺省为 "tenant_id"
db := tenant.Scope(gormDB, ctx, "org_id")  // 自定义列名
// 上下文无租户时返回的 db 已注入 ErrTenantNotFound，后续执行必然报错 —— 同样 fail-closed
```

确需跨租户的全局任务（租户管理、数据汇总、定时巡检）必须**显式豁免**：

```go
// 方式一：派生一个跳过过滤的 ctx（保留租户信息，仅供展示）
db.WithContext(tenant.WithSkip(ctx)).Find(&tenants)

// 方式二：块级豁免，fn 执行期间关闭隔离
err := tenant.ExecuteIgnore(ctx, func(ctx context.Context) error {
    return db.WithContext(ctx).Find(&allTenants).Error
})
```

豁免是**显式声明**的——代码评审时 `WithSkip`/`ExecuteIgnore` 出现的位置就是需要重点检查越权风险的位置。

### 9.5 租户隔离边界速查

| 场景 | 行为 |
| --- | --- |
| 无租户上下文查询带 `tenant_id` 的表 | 报错（`ErrContextMissing`），不执行 |
| 创建时未赋值 `tenant_id` | 自动填充为上下文租户 |
| 创建时显式赋了 `tenant_id` | 保留显式值（不覆盖） |
| 全局表（无 `tenant_id` 字段） | 插件不介入 |
| `Joins` 查询 | 插件跳过，需手动 `Scope` |
| `WithSkip` / `ExecuteIgnore` | 跳过填充与过滤 |

---

## 10. 中间件编排最佳实践

### 10.1 推荐路由分层（对齐 devops-system 实践）

```go
func Register(...) httpinfra.Register {
    return func(r *gin.Engine) {
        // 公开端点：JWKS、登录、刷新（放最前，避免被认证拦截）
        r.GET("/.well-known/jwks.json", jwksHandler)
        api.POST("/auth/login", wrap(authH.Login))
        api.POST("/auth/refresh", wrap(authH.Refresh))

        // 第一层：登录态（仅 JWT）
        logged := api.Group("", securitymw.GinAuthenticate(authn, opts))
        logged.POST("/auth/logout", wrap(authH.Logout))

        // 第二层：登录态 + 租户上下文
        biz := logged.Group("", tenant.Middleware(true))

        // 第三层：按路由粒度挂权限
        users := biz.Group("/users")
        users.GET("", securitymw.GinRequirePermission(authz, "system:user:list", opts), wrap(userH.List))
        users.POST("", securitymw.GinRequirePermission(authz, "system:user:create", opts), wrap(userH.Create))

        // 平台路由：JWT → PlatformPerm，不挂 tenant 中间件（操作全局表）
        plat := logged.Group("/tenants")
        plat.GET("", securitymw.GinRequirePlatformPermission(authz, "platform:tenant:list", opts), wrap(tenantH.List))
    }
}
```

### 10.2 检查清单

- [ ] **顺序**：`GinAuthenticate` → `tenant.Middleware` → `GinRequirePermission` → handler，权限中间件永远先于 handler 注册；
- [ ] **信封一致**：`GinOptions` 回调统一走 `response.Fail`，与业务错误同一格式；
- [ ] **503 不是 401**：Redis/Casbin 故障走 `OnInfrastructureError` 返回 503，别把基础设施故障伪装成未登录误导前端跳登录页；
- [ ] **每个请求都过会话校验**：不要为了性能绕过 `SessionStore.Active`——这是踢下线即时生效的关键；
- [ ] **平台权限标识隔离**：`platform:*` 权限只挂平台路由，不进租户菜单表；
- [ ] **豁免显式化**：`WithSkip`/`ExecuteIgnore` 只允许出现在全局管理代码，业务查询禁止；
- [ ] **刷新轮换**：refresh 端点先建新会话再撤销旧 sid，撤销失败仅记日志（旧会话自然过期）；
- [ ] **跨服务传递**：把租户上下文放进 gRPC metadata / 消息头，消费端用 `tenant.With` 重建，不要依赖隐式传递。

---

## 11. 单元测试支持

安全组件全部面向接口设计，stub 即可测试，无需真实 Redis / Casbin：

```go
type stubTokenParser struct{ claims *securityjwt.Claims; err error }
func (s *stubTokenParser) ParseAccess(token string) (*securityjwt.Claims, error) { return s.claims, s.err }

type stubSessionStore struct{ active bool; err error }
func (s *stubSessionStore) Active(ctx context.Context, ref securitysession.SessionRef) (bool, error) {
    return s.active, s.err
}

type stubAuthorizer struct{ enforceOK, platformAdmin bool; _ error }
func (s *stubAuthorizer) Enforce(ctx context.Context, sub, dom, obj string) (bool, error) { return s.enforceOK, nil }
func (s *stubAuthorizer) EnforcePlatform(ctx context.Context, sub, obj string) (bool, error) { return true, nil }
func (s *stubAuthorizer) IsPlatformAdmin(ctx context.Context, sub string) (bool, error) { return s.platformAdmin, nil }
```

完整可编译示例见 `security/middleware/gin_test.go`（覆盖缺失头、非法 token、会话失效、基础设施错误、权限拒绝等全部分支）。会话存储测试可用 `miniredis`（devops-infra 测试依赖已内置）。Gin 路由测试用 `gin.TestMode` + `httptest`。

---

## 12. 附录：API 速查表

### HTTP（`devops-infra/http`）

| API | 说明 |
| --- | --- |
| `http.Run(cfg, register, readiness...)` | 启动 HTTP 服务（Recovery/AccessLog/healthz/readyz/优雅退出） |
| `http.ReadinessCheck{Name, Check}` | 就绪检查项 |
| `middleware.Recovery()` | panic → 500 统一信封 |
| `middleware.AccessLog()` | 结构化访问日志（Debug） |

### 响应（`devops-infra/http/response`）

| API | 说明 |
| --- | --- |
| `response.OK / OKPage` | 成功信封 / 分页信封 |
| `response.Fail / FailErr` | 失败出口 / 错误统一出口 |
| `response.Wrap(handler)` | `func(c) error` → `gin.HandlerFunc` |
| `response.BaseResponse[T] / PageData[T]` | 信封结构体 |

### gRPC（`devops-infra/grpc`）

| API | 说明 |
| --- | --- |
| `grpc.Run(cfg, register)` | 启动 gRPC 服务（Recovery/AccessLog/health/reflection/优雅退出） |
| `grpc.Dial(ctx, endpoint, timeout)` | 非阻塞拨号，内置 Unavailable 重试 |
| `grpc.IsUnavailable(err) / Close(conn)` | 错误判定 / 关闭连接 |
| `interceptor.Recovery() / AccessLog()` | 单独取用拦截器 |

### JWT（`devops-infra/security/jwt`）

| API | 说明 |
| --- | --- |
| `NewManager(Config) (*Manager, error)` | 构造签名管理器 |
| `Manager.Issue(ctx, Subject, sid) (TokenPair, error)` | 签发 RS256 access token |
| `Manager.ParseAccess(token) (*Claims, error)` | 验签解析 |
| `Manager.JWKS() (map[string]any, error)` | 输出 JWKS 文档 |
| `PublicKeyFromPrivatePEM(pem) (*rsa.PublicKey, error)` | 私钥导公钥 |
| `NewStaticKeySet(map[kid]*rsa.PublicKey)` | 静态公钥集合 |
| `JWKSDocument(pub, kid)` | 单独生成 JWKS |

### 会话（`devops-infra/security/session`）

| API | 说明 |
| --- | --- |
| `NewRedisStore(cacheClient, ttl) / NewRedisClientStore(goRedis, ttl)` | 构造存储 |
| `Store.Create / Active / Lookup / Matches` | 建会话 / 活性 / 反查 / 归属校验 |
| `Store.Revoke / RevokeUser / RevokeTenant` | 单个 / 用户全部 / 租户全部撤销 |

### 认证鉴权中间件（`devops-infra/security/middleware`）

| API | 说明 |
| --- | --- |
| `NewAuthenticator(TokenParser, SessionStore) *Authenticator` | 框架无关认证器 |
| `Authenticator.Authenticate(ctx, bearerHeader)` | 认证决策，错误三类可 `errors.Is` |
| `GinAuthenticate(authn, opts)` | Gin 认证中间件 |
| `GinRequirePermission(authz, perm, opts)` | 域内鉴权（uid × tid × perm） |
| `GinRequirePlatformPermission(authz, perm, opts)` | 平台鉴权（g2 + dom="*"） |
| `CurrentClaims(c) *securityjwt.Claims` | handler 中取当前身份 |
| `GinOptions{OnUnauthorized, OnForbidden, OnInfrastructureError}` | 错误呈现回调 |

### Casbin（`devops-infra/security/authz`）

| API | 说明 |
| --- | --- |
| `New(gormDB) (*CasbinAuthorizer, error)` | 初始化（gorm-adapter 持久化） |
| `Enforce / EnforcePlatform / IsPlatformAdmin` | 鉴权决策 |
| `RebuildRolePolicies / RemoveRolePolicies` | 角色权限快照重建 / 清除 |
| `RebuildUserGrouping / RemoveUserGrouping / RemoveRoleGrouping` | 用户-角色绑定维护 |
| `AddPolicyIfAbsent("p"/"g"/"g2", values...)` | 幂等加策略（种子数据） |

### 多租户（`devops-infra/tenant`）

| API | 说明 |
| --- | --- |
| `Middleware(required) / MiddlewareWithHeader(header, required)` | Gin 租户注入中间件 |
| `With / WithTenant / From / TenantID` | 上下文注入与读取 |
| `RequiredTenantID / RequiredTenantCode` | 严格读取（缺失即错） |
| `RegisterPlugin(gormDB)` | GORM 自动填充 + 行级过滤回调 |
| `Scope(db, ctx, column)` | 手动追加租户条件 |
| `WithSkip / ExecuteIgnore / IsSkip` | 受控豁免 |

### 错误码（`devops-infra/errs`）

| API | 说明 |
| --- | --- |
| `CodeOK/Param/Unauthorized/Forbidden/NotFound/MethodNotAllowed/Internal` | 与 HTTP 状态码对齐 |
| `errs.New(code, msg) / Wrap(err, code, msg)` | 业务错误构造（保留底层原因） |
| `errs.ErrParam / ErrUnauthorized / ErrForbidden / ...` | 预定义错误 |

---

*本文对应 devops-infra 当前实现，代码示例以包内实际导出 API 为准；参考实现见 `devops-system/internal/{auth,authz,middleware,router,tenantdb}`。*
