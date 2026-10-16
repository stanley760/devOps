# devops-user（C 端用户服务）

与 `devops-system`（B 端管理后台）并列的**独立服务**，面向 App / 小程序等 C 端用户。

## 网关侧配置

C 端走**独立域名 + 独立 jwt-auth 插件实例**，与 B 端完全隔离：

| 资源 | 文件 | 作用 |
|---|---|---|
| 主路由 | `deploy/higress/ingresses/devops-user.yaml` | `user-api.devops.local/user`（挂 jwt-auth） |
| 匿名路由 | `deploy/higress/ingresses/devops-user-public.yaml` | login / register / jwks.json（豁免） |
| 验签插件 | `deploy/higress/wasmplugins/jwt-auth-user-2.0.0.yaml` | `issuer: devops-user` |

**为什么独立域名**：两个服务都提供 `/.well-known/jwks.json`，同 host 下最长前缀匹配
无法区分归属会随机命中其一。且 C/B 端流量特征不同（用户量、限流维度、灰度需求），
需要分别治理。

### 三个不可改项（都踩过坑）

1. **`metadata.name` 不能叫 `jwt-auth`** —— pilot 会走 Istio 原生分支
   （`JwtRule: ""`），all-in-one 2.2.3 无 `jwt_authn` filter，一律不加载。
2. **`url` 必须用 `localhost:8002`** —— `oci://` 需 controller 拉取落盘，file 后端不触发。
3. **`matchRules.config` 不能内联 `consumers` 数组** —— pilot 解析失败并**静默跳过
   整个插件**（无任何日志）；也不能只写 `{}`（会整体替换 `defaultConfig`，
   真 token 也被 403 `Access Denied`）。必须带 `allow` 指回 `defaultConfig` 的 consumer。

验证插件是否真加载：

```bash
docker compose -f deploy/docker-compose-dev.yml exec higress \
  sh -c 'grep -c "fetching image plugins/jwt-auth" /var/log/higress/gateway.log'
```

或看无 token 请求返回**纯文本** `Jwt is missing`（信封格式 `{"code":401,...}`
说明是应用层，插件没生效）。

### 匿名路径不可合并

`devops-user-public.yaml` 里三条路径必须写全：

- 合并成 `/user/api/v1/auth` → `logout` 一并豁免（任何人都能踢人下线）
- 合并成 `/user/api/v1` → `profile` 一并豁免（**严重越权**）

### 跨端 token 隔离已实测

C 端与 B 端 issuer 不同，B 端 token 访问 C 端接口返回 `Jwt verification fails`，
反向同理——这是 issuer 隔离的真实效果，勿把任一方 issuer 改成相同值。

## 快速开始

```bash
cd devops-user
cp config/.env.example config/.env   # 或手写，见下
go run . server
# 打开 http://127.0.0.1:10000/apidoc 查看交互式接口文档
```

`config/.env` 需要设置 `NACOS_REGISTER_IP`（网关据此回连本进程）：

```bash
# Windows + Docker Desktop
NACOS_REGISTER_IP=host.docker.internal
```

> ⚠️ **不能填网桥网关（如 172.19.0.1）**：实测容器访问 `172.19.0.1:10000` 返回 000
> （不可达），而 `host.docker.internal:10000` 返回 200。
> Linux 下改填宿主机内网 IP（`ip addr` 查到的非回环地址）。

`.env` 已被 `.gitignore` 的 `**/config/.env` 覆盖，不会误提交。

## 与 devops-system 的关系

**框架能力完全复用 `devops-infra`，零复制**：

| 能力 | 位置 |
|---|---|
| HTTP 引擎 / 优雅关闭 / `/metrics` | `devops-infra/http` |
| 表基模型 BaseModel（主键+审计+软删+发号钩子） | `devops-common/model`（与 devops-system 共用） |
| OpenAPI 装配与文档端点 | `devops-infra/http/apidoc` |
| 统一响应信封 | `devops-common/response` |
| 错误出口与状态码 | `devops-infra/errs` + `http/humaerr` |
| JWT 中间件套件 `Kit` | `devops-infra/security/humakit` |
| RS256 签发验签 | `devops-infra/security/jwt` |
| Redis 会话态 | `devops-infra/security/session` |
| 流量入口守卫 | `devops-infra/security/gateway` |

**业务形态上的差异（有意为之，不是遗漏）**：

| 维度 | devops-system（B 端） | devops-user（C 端） |
|---|---|---|
| 接口前缀 | `/system/api/v1/*` | `/user/api/v1/*` |
| 端口 | 9999 | 10000 |
| Redis 库 | db 0 | db 1（避免键空间污染） |
| 租户模型 | 单库多租户（`TenantPlugin` + casbin） | **无**（单租户） |
| 权限 | RBAC 权限点（`kit.Perm("system:user:list")`） | **无**，用 `kit.Logged()` + service 层按 `claims.UID` 行级过滤 |
| JWT issuer / kid | `devops` / `devops-system-rs256-1` | `devops-user` / `devops-user-rs256-1` |

> ⚠️ **issuer 必须与 devops-system 不同**。两者 issuer 相同时，在网关 jwt-auth 的
> consumers 里会互相匹配，导致 B 端 token 能访问 C 端接口（反之亦然）。

## C 端行级归属校验

不引入 RBAC。接口**不接受**客户端传入的用户 ID，身份只来自 JWT：

```go
//api/v1/profile.go —— updateBody 没有 id / user_id 字段
type updateBody struct {
    Nickname *string `json:"nickname" required:"false"`
    // ...
}

// service 从 ctx 取 claims，天然只能改自己的数据
func (s *Profile) Update(claims *auth.AccessClaims, in UpdateInput) (...) {
    s.users.update(claims.UserID, func(u *model.User) { /* ... */ })
}
```

若允许客户端传 ID 再比对，等于把越权判断交给客户端参数。

## 当前实现状态

已跑通：**注册 → 登录 → 读资料 → 改资料 → 登出**，含 OpenAPI 文档与 JWKS 端点。
数据已落到 MySQL `t_user` 表（启动期自动建表），主键由 devops-id 发号。

### 已完成

- **GORM 接入**：`model`（表模型 + 发号钩子）、`dao`（数据访问）、
  `dbinit`（启动期建表，内嵌 SQL 脚本）。
- **bcrypt 口令哈希**（cost=10），并显式拒绝超过 72 字节的口令（bcrypt 上限）。
- **三个唯一约束**：`uk_username` / `uk_email` / `uk_mobile`，
  冲突时按**具体约束**给出不同文案（不笼统报「用户名已被注册」）。

### 待补齐

- **三方 token 未加密入库**：`t_user_oauth.access_token` / `refresh_token`
  当前明文存储，生产必须改应用层 AES-GCM 加密（主密钥进密钥管理）。
- **改密「保留本设备」不完全**：`ChangePassword` 支持保留当前设备的 refresh，
  但 **access 侧仍会全踢**（`session.Store` 只有 `user → sids` 正向索引，
  没有 `sid → refreshHash` 反查，无法定位要保留的那个会话），
  故保留设备的用户仍需重新登录一次，只是 refresh 还在可直接换新 access。
- **封禁无管理端入口**：`Profile.SetDisabled` / `SetDisabledByUsername` 已在
  service 层就绪并强制全端下线，但**故意不暴露 HTTP 端点**——
  C 端用户若能自助解封等于封禁无效。待管理端（devops-system）或运营后台接入。

## 三方登录（OAuth2）

支持 GitHub / Google / 微信（开放平台网站应用扫码）三种提供方，标准授权码流程。

### 接口

| 端点 | 鉴权 | 说明 |
|---|---|---|
| `GET /user/api/v1/oauth/providers` | 匿名 | 列出可用提供方与授权地址（含一次性 state） |
| `GET /user/api/v1/oauth/{provider}/callback` | 匿名 | query 模式回调（**三家均走这个**） |
| `POST /user/api/v1/oauth/{provider}/callback` | 匿名 | form_post 模式回调（当前无提供方使用，留作预留） |
| `POST /user/api/v1/oauth/bind/start` | 需登录 | 已登录用户发起绑定 |
| `GET /user/api/v1/oauth/bind` | 需登录 | 列出已绑定（不含任何 token） |
| `DELETE /user/api/v1/oauth/bind` | 需登录 | 解绑 |

### 两种登录路径

由 state 里的 `BindUserID` 决定：

- **登录**（`BindUserID == 0`）：已有绑定 → 直接签发双 token；无绑定 → 按
  `auto_register` 决定自动建号还是报「请先登录后绑定」。
- **绑定**（`BindUserID > 0`）：挂到当前登录用户。若三方账号已绑给别人则拒绝
  （`uk_provider_provider_id` 唯一索引双重保障）。

### 五个安全要点

1. **state 必须校验且一次性**（Lua `GETDEL` 原子实现）——不校验则登录可被 CSRF 劫持；
   不一次性则同一 state 可重放。实测伪造/无 state 均 401。
2. **`provider_user_id` 用三方稳定标识**（GitHub 数字 ID、Google `sub`、微信 `unionid`），
   **不用 username/email/nickname**——前两者可改可换，微信昵称可重复，
   用作绑定键会引发错绑与冒用。
3. **Google 走 tokeninfo 校验 aud/iss**；**微信必须显式判 `errcode`**——
   ⚠️ 微信**所有接口出错也返回 HTTP 200**，错误只写在 body 的 `errcode` 里。
   只判状态码会把「invalid code」当成功，进而写入空绑定键使账号互相错绑。
4. **跳回地址必须过白名单**（`oauth.redirect_whitelist`）——否则 open redirect 钓鱼：
   受害者登录后被跳到攻击者站点偷 token。白名单为空时**一律拒绝**。
5. **自动建号的账号无可用口令**：生成 32 字节随机强密码，杜绝「空密码/默认密码」被登录。

### 微信的四个特殊约束

微信授权端点**不是标准 OAuth2 端点**，故 `WeChatProvider` 手工拼 URL，
不使用 `oauth2.Config.AuthCodeURL`：

| 约束 | 说明 |
|---|---|
| 端点是 `/connect/qrconnect` | 不是常见的 `/oauth2/authorize` |
| URL 末尾须带 `#wechat_redirect` | **缺了微信直接提示「该链接无法访问」** |
| `redirect_uri` 只编码一层 | `url.Values.Encode()` 会把 `%` 编成 `%25` 造成双重编码，故手工拼 |
| `scope` 固定 `snsapi_login` | 其他值一律被拒；配了 `scopes` 也会被内部强制覆盖 |

绑定键取 `unionid` 优先（同一开放平台账号下跨应用唯一），缺失时退回 `openid`；
两者皆空则**拒绝登录**。

⚠️ **微信不返回邮箱且不允许索取**，故纯微信用户的 `email` 恒为空——
「邮箱找回密码」对其不可用，需另设绑定手机号/邮箱流程。
头像 URL 在用户改头像后即失效，当前仅作展示快照。

### 数据表

`t_user_oauth`，两个唯一索引缺一不可：

- `uk_provider_provider_id`——防「一个三方账号绑多个本地用户」（账号劫持）
- `uk_user_provider`——防「同一用户重复绑定同一提供方」

另有本地用户表 `t_user`（与 `t_system_users` 的差异：**无 `tenant_id`**、
无角色/部门、`disabled` 取代 `status`）。⚠️ 它的邮箱/手机号必须用指针字段
（`*string`），未填写时存 `NULL` 而非空串——MySQL 唯一索引不约束多个 `NULL`，
若用空串则第二个未绑定邮箱的用户即冲突（应用层 `model.NormalizePtr` 负责归一）。

### ⚠️ 当前限制

- **微信未用真实凭据跑通端到端**：接口实现与单元测试已完备（12 项，含 errcode
  与空标识防护），但受「须企业资质 + 已备案域名」限制，尚无真实 AppID/Secret
  做真实扫码验证。接入步骤见 `docs/oauth2-guide.md`。
- **三方 token 未加密**：见「待补齐」。
- **微信用户无邮箱**：见上文微信约束。

## 认证体系

### 双 token（access + refresh）

| | access | refresh |
|---|---|---|
| 形态 | RS256 JWT | **256 位随机不透明串**（base64url 43 字符） |
| 存储 | 无状态 | Redis，服务端**只存 SHA-256 哈希** |
| 有效期 | `jwt.access_expire`（默认 2h） | `jwt.refresh_expire`（默认 168h） |
| 失效方式 | 等过期 | 登出 / 轮换 / 改密时精确作废 |

> ⚠️ **切勿把 access 当 refresh 复用**：access 是无状态 JWT，拿到就能用到过期，
> 等于给了攻击者无限续命的凭据，且登出/封禁无法真正作废它。

轮换（`POST /user/api/v1/auth/refresh`）用 **Lua 脚本原子**完成
「查旧 → 删旧 → 写新」——分步执行会让并发刷新各自拿到同一个旧 token 并各自签发新
token（重放漏洞）。实测：用过的 refresh 立即返回 401。

登出时若带上 `refresh_token` 会一并作废，否则旧 refresh 仍能换新 token（等于没登出）。

### 登录失败限流与账号锁定

bcrypt 只能拖慢单次爆破，无法阻止高速尝试。C 端开放注册、用户名可枚举，
故叠加两个维度（**缺一可被绕过**）：

| 维度 | 默认阈值 | 拦什么 |
|---|---|---|
| IP 限流 | 5 分钟内20 次失败 → 429 | 一个 IP 批量试不同用户名 |
| 账号锁定 | 15 分钟内 5 次失败 → 锁定 15 分钟 | 多个 IP 集中试同一账号 |

配置见 `login_lock`段。客户端 IP 经 `server.trusted_proxies` 校验 XFF 后才采用——
否则攻击者伪造 `X-Forwarded-For` 即可绕过 IP 限流。

「用户不存在」也计一次失败（否则可无成本枚举用户名）；账号已封禁**不计**失败
（不是用户输错，计入会误封正常用户的 IP 计数）。

### 匿名端点（必须网关豁免 + 应用层放行）

| 路径 | 为什么匿名 |
|---|---|
| `POST /user/api/v1/auth/register` | 注册 |
| `POST /user/api/v1/auth/login` | 此刻还没有 token |
| `POST /user/api/v1/auth/refresh` | **此刻 access 已过期**，要求携带 JWT 等于永远刷不出来 |
| `GET /.well-known/jwks.json` | 公钥分发 |

⚠️ logout / profile / profile/password **不可**合并进这些前缀：
合并成 `/user/api/v1/auth` 会把 logout 一并豁免（任何人都能踢人下线）；
合并成 `/user/api/v1` 会把 profile 与改密一并豁免（**严重越权**）。

## 目录结构

```
devops-user/
├── main.go                # humaerr.Install + banner + cmd.Execute
├── banner.txt             # go:embed
├── config/
│   ├── bootstrap.yaml         # 公共基线（server/nacos）
│   ├── devops-user-dev.yaml   # 环境覆盖（含 dev JWT 私钥）
│   └── .env               # NACOS_REGISTER_IP（不提交）
└── internal/
    ├── cmd/root.go        # 装配：db → 建表 → 发号 → Redis → JWT → Kit → service
    ├── config/            # 根 Config 与专有配置段
    ├── auth/              # 认证门面（C 端语义适配）
    ├── middleware/        # Kit 别名 + 中文信封文案
    ├── model/             # GORM 表模型（BaseModel 复用 devops-common/model）
    ├── dao/               # 数据访问（无租户过滤）
    ├── dbinit/            # 启动期建表（内嵌 sql/schema）
    ├── service/           # 业务用例 + bcrypt
    ├── api/v1/            # huma handler（含 ClientIP 中间件）
    ├── refreshtoken/      # refresh token 存取（不透明串 + 哈希，Lua 原子轮换）
    ├── loginlock/         # 登录失败限流与账号锁定
    ├── router/            # 路由注册
    └── store/             # Redis 装配（委托 infra/cache/redis）
```

## roadmap

- [x] 用户登录注册，oauth2.0 三方登录 github, google, wechat
- [ ] 用户信息管理，修改密码，修改邮箱，修改手机号，修改头像，修改昵称
- [ ] 用户画像分析，例如
  - [ ] 用户登录次数，登录时间，登录IP，登录设备，登录地理位置
  - [ ] 用户注册时间，注册IP，注册设备，注册地理位置
  - [ ] 用户活跃度分析，例如日活跃用户数，周活跃用户数，月活跃用户数
  - [ ] 用户留存率分析，例如次日留存率，7日留存率，30日留存率