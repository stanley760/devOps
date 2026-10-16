# Higress 网关配置与部署

本目录是 Higress 网关的**配置唯一真源**：`mcpbridges/`、`ingresses/`、`wasmplugins/`
是 Higress 文件配置后端识别的资源目录，同时这些 yaml 也是迁移到 Kubernetes 时的
清单参考。本文档描述各功能的配置方法，以及 dev / docker 生产 / K8s 三种部署形态。

```
deploy/higress/
├── mcpbridges/default.yaml          # 服务来源：Nacos 注册中心对接（devops-nacos）
├── ingresses/devops-system.yaml     # 路由：api.devops.local -> devops-system
├── ingresses/devops-system-canary.yaml  # 灰度路由（10% 流量到 gray 实例）
├── ingresses/devops-system-gray-route.yaml  # 灰度专用域名：gray.devops.local 100% 直达
├── services/higress-gateway.yaml    # all-in-one 内置 Service 声明（K8s 迁移参考）
├── wasmplugins/cors-1.0.0.yaml      # 跨域：网关统一处理（控制台导出格式）
└── wasmplugins/geo-ip-1.0.0.yaml    # GeoIP：注入 X-Higress-Geo-* 头（登录审计消费）
```

> `.gitignore` 采用白名单：只跟踪 `mcpbridges/`、`ingresses/`、`wasmplugins/` 下的
> yaml。控制台运行时写入 `/data` 的状态文件（configmaps/secrets/envoyfilters 等）
> 不入库，见「配置管理约定」。

---

## 一、架构与部署形态

| 形态 | 镜像/方式 | 适用 | 文档章节 |
|---|---|---|---|
| dev | all-in-one 单容器 + 命名卷 | 本地开发 | 本文「二」 |
| docker 生产 | controller + gateway + console 分离 + Nacos 配置后端 | 单机/小规模生产 | 本文「八」 |
| K8s | Helm 部署（见 `deploy/k8s/devops.yaml`） | 正式生产推荐 | 本文「九」 |

无论哪种形态，业务流量路径一致：

```
Client ──▶ Higress Gateway(:8080/:8443)
              │  jwt-auth / cors / cluster-key-rate-limit / geo-ip
              ▼
           Nacos 服务发现 ──▶ devops-system / devops-system-gray（注册于 DEVOPS_GROUP）
```

**安全边界**：`devops-system:9999` 不发布到宿主机端口或公网负载均衡；运维端点
（`/metrics` `/apidoc` `/healthz` `/readyz`）不经公网网关暴露，Ingress 只路由
`/system` 与 `/.well-known/jwks.json`。

> ⚠️ 「不发布到宿主机」只是**第一道**防线，不足以保证流量入口统一。
> 同网络内的任何容器（如 MySQL、Redis、Nacos 容器）都能直连 `devops-system:9999`
> 绕过网关，此时网关层能力全部失效：限流失效、GeoIP 注头消失（登录审计的
> `X-Higress-Geo-*` 四字段全空）、灰度失效、CORS 失效。
> 因此本项目采用**三层收口**，详见第七章 7.1。

---

## 二、Dev 环境（all-in-one + 命名卷）

### 2.1 为什么用命名卷而不是 bind mount

all-in-one 容器持续向 `/data` 写状态（pilot、console、内置可观测组件）。Windows
下 bind mount 跨 NTFS→WSL2 文件系统边界，高频小写入会把 CPU 打满到 Docker 引擎
完全无响应（500 错误）。因此 dev compose 使用命名卷 `higress-data`。

### 2.2 配置灌入与同步

命名卷首次为空，配置灌入由 compose 的 `higress-init` 一次性服务完成：它在 higress
启动前把本目录 `mcpbridges/`、`ingresses/`、`wasmplugins/` 三个 git 管理目录复制进
命名卷（`docker-compose-dev.yml` 中定义，busybox 镜像，跑完即退出）。因此：

- `up` 只在卷为空/`down` 后重跑 init 灌入（compose 不会重跑已完成的容器）；
- 修改本目录 yaml 后的显式同步（停网关 → 重灌 → 修毒配置 → 起网关）：

```bash
sh deploy/dev.sh sync       # 或 .\deploy\dev.ps1 -Command sync
# 强制重跑 higress-init 灌入，并删除 higress-config 里自动再生的
# inclusionRegexps: [.*]（见 2.3 坑表），然后重启网关
```

> init 每次都会覆盖这三个目录。**约定：配置一律改本目录 yaml 再 sync，控制台只用来
> 查看。** 在控制台里做的路由/插件改动请先移回 yaml，否则下次 up 会被覆盖丢失。
> 控制台写入 `/data` 其他位置的状态（configmaps/secrets 等）不受影响。

### 2.3 已知坑（排查手册）

| 症状 | 原因 | 解法 |
|---|---|---|
| 控制台「服务来源」显示 `higress-console` 而非 `devops-nacos` | 卷里是 all-in-one 首次启动生成的默认 McpBridge，git 配置没灌入 | 执行 `sync`；确认 `/data/mcpbridges/default.yaml` 里是 `devops-nacos` |
| 路由 404 / Ingress 不生效 | 同上，`/data/ingresses/` 为空 | 同上 |
| Docker 引擎 500 / CPU 异常高 | ① Windows bind mount（已改命名卷）；② all-in-one 内置 prometheus/loki/promtail/grafana 与外部栈重复运行；③ `higress-config` 里 `proxyStatsMatcher.inclusionRegexps: [.*]` 导致 Envoy 全量高基数指标 | 用命名卷；禁用内置可观测组件（见「八、可观测性」）；`sync` 已内含该修复（见下行） |
| envoy 启动 ~1 分钟后消失，8080 TCP 通但空回复（8001 正常） | `higress-config` 的 `inclusionRegexps: [.*]`（**新卷每次首启都会自动重新生成**，且文件为 CRLF 行尾）→ envoy 统计表爆内存，被 cgroup OOM 杀；supervisord 视其 exit 0 为 expected 不自动拉起 | `sh deploy/dev.sh sync`（停机删配置再启动）；dev compose 内存上限已提 2g 兜底 |
| all-in-one 内部各进程批量 exited | 容器停止/重启的 supervisor shutdown 序列，日志形如 `exited: prometheus (exit status 0; expected)` | 先看 `docker compose ps` 确认容器是否在重启循环 |

---

## 三、服务来源：McpBridge（Nacos 对接）

`mcpbridges/default.yaml` 定义 Higress 从注册中心发现后端服务：

```yaml
apiVersion: networking.higress.io/v1
kind: McpBridge
metadata:
  name: default
  namespace: higress-system
spec:
  registries:
    - name: devops-nacos        # 服务来源名称，控制台「服务来源」页显示此项
      type: nacos2              # nacos2 = Nacos 2.x gRPC 协议，服务变更感知秒级
      domain: nacos             # compose 网络内服务名；K8s 时改为实际 Service 地址
      port: 8848
      nacosNamespaceId: public   # 业务服务注册的命名空间
      nacosGroups:
        - DEVOPS_GROUP          # 业务服务注册的分组
```

要点：

- **下划线转义规则**：Nacos 分组 `DEVOPS_GROUP` 在 Higress 目标地址中写作
  `DEVOPS-GROUP`（Higress 对下划线的转义规则），Ingress 的
  `higress.io/destination` 里要用转义后的写法。
- `type: nacos2` 需要 Nacos 同时开放 gRPC 端口（8848 同步偏移 +1000，即 9848），
  compose 中 nacos 服务需 `expose` 9848/9849。
- 灰度实例 `devops-system-gray` 注册到**同一**分组，靠服务名区分，无需额外来源。
- 多注册中心时在 `registries` 下并列多项（如再接一个 DNS 类型的静态来源）。

---

## 四、路由：Ingress

`ingresses/devops-system.yaml`：

```yaml
apiVersion: networking.higress.io/v1  # 实际为 networking.k8s.io/v1，见文件
kind: Ingress
metadata:
  name: devops-system
  namespace: higress-system
  annotations:
    # 目标地址格式：<服务名>.<NACOS分组转义>.<命名空间>.nacos
    higress.io/destination: devops-system.DEVOPS-GROUP.public.nacos
spec:
  ingressClassName: higress
  rules:
    - host: api.devops.local
      http:
        paths:
          - path: /api
            pathType: Prefix
            backend:
              resource:
                apiGroup: networking.higress.io
                kind: McpBridge
                name: default
```

要点：

- `higress.io/destination` 指向 McpBridge 发现的服务，服务实例上下线由 Nacos
  自动感知，无需改路由。
- `host` 是对外域名；本地验证用 `curl -H 'Host: api.devops.local'
  http://localhost:8080/healthz` 或加 hosts 映射。
- 只路由业务 API 与 JWKS，**不要**把 `/metrics` 等运维端点挂到公网网关。
- Higress 支持标准 Ingress 注解（rewrite、timeout 等）与 `httproutes`（新协议），
  新增能力优先查 Higress 官方注解文档再决定是否引入自定义 EnvoyFilter。

---

## 五、灰度发布

`ingresses/devops-system-canary.yaml`：同 host、同 path 的 Ingress 加 canary
注解即可分流：

```yaml
metadata:
  annotations:
    higress.io/canary: "true"
    higress.io/canary-weight: "10"        # 权重灰度：约 10% 流量
    higress.io/destination: devops-system-gray.DEVOPS-GROUP.public.nacos
```

三种分流方式，**优先级 Header > Cookie > 权重**：

```yaml
higress.io/canary-by-header: "x-canary"           # 指定 Header 灰度
higress.io/canary-by-header-value: "true"          # 值完全匹配才进灰度
higress.io/canary-by-cookie: "canary"              # Cookie 灰度
higress.io/canary-weight: "10"                     # 兜底权重灰度
```

操作流程：`gray` 实例以 `devops-system-gray` 为服务名注册到同一 Nacos 分组 →
修改 canary yaml 权重 → `sync`（dev）或应用清单（生产）→ 观察 Grafana/日志确认
灰度流量比例 → 全量时把新版本以 `devops-system` 名义发布并删除 canary 路由。

**灰度实例定向访问**（不走权重随机）：`ingresses/devops-system-gray-route.yaml` 提供
`gray.devops.local` 专用路由，100% 直达灰度实例，用于灰度版本验证。本地启动灰度实例：

```bash
cd devops-system && go run . server -f config/bootstrap-gray.yaml   # :9998，注册名 devops-system-gray
curl -i -H 'Host: gray.devops.local' http://localhost:8080/api/v1/auth/info   # 401 即已达后端
```

注意：控制台「路由管理」不把 canary Ingress 作为独立条目展示（它合并为主路由的
分流规则），灰度是否生效以 envoy 实际配置/流量为准：
`docker compose -f deploy/docker-compose-dev.yml exec higress sh -c 'curl -s localhost:15000/config_dump | grep -c devops-system-gray'`。

---

## 六、Wasm 插件

插件通过 `matchRules.ingress` 绑定到具体路由，`defaultConfigDisable: true` 表示
未匹配的路由不生效。

**路由名不带 namespace 前缀**：all-in-one 的 file 后端以文件名作资源名生成路由，
envoy 中的 route name 是 `devops-system`（而非 K8s 模式下的
`higress-system/devops-system`）。matchRules 写成 `higress-system/xxx` 永远匹配
不上，插件会**静默不动作**（无任何日志）——这是本次踩坑后实测确认的。验证插件
是否真的执行：预检 OPTIONS 期望 204 + `access-control-*` 头；限流看响应里的
`x-ratelimit-*` 头。

**控制台「插件配置」页不显示手写 yaml 为"生效"属正常现象**：该页面只识别带
`higress.io/resource-definer: higress`、`higress.io/wasm-plugin-built-in: "true"`、
`higress.io/wasm-plugin-name` 等 label 的资源（即从控制台创建/导出的，如
geo-ip-1.0.0.yaml / cors-1.0.0.yaml，均为控制台导出格式含 label）；手写的
cluster-key-rate-limit.yaml 无这些 label，控制台不归类展示，但 envoy
中照常加载执行。以 `config_dump` 为准：
`docker compose -f deploy/docker-compose-dev.yml exec higress sh -c 'curl -s localhost:15000/config_dump' | grep higress-system.cors`。

### 6.1 跨域（cors.yaml）

网关统一处理 CORS，**应用侧不再实现 CORS**，避免响应头重复。新增来源域名时改
`allow_origins` 后同步生效：

```yaml
matchRules:
  - ingress: [devops-system, devops-system-gray-route]   # 路由名，无 namespace 前缀
    config:
      allow_origins: ["http://localhost:5173", "http://localhost:3000"]
      allow_methods: [GET, POST, PUT, PATCH, DELETE, OPTIONS]
      allow_headers: [Content-Type, Authorization, X-Requested-With]
      allow_credentials: true
```

### 6.2 集群限流（cluster-key-rate-limit.yaml）

基于 Redis 的**集群级**限流（多网关实例共享计数），当前策略为每 IP 100 QPS，
超限返回 429。

两个必踩的坑：

1. **matchRules 命中后的 config 整体替换 defaultConfig**，`redis` 连接块必须在每条
   命中规则里重复携带，否则插件 `missing redis in config` 加载失败且 fail-closed。
2. **`redis.service_name` 指向 Higress 服务来源里的服务**，不是 compose 服务名。
   compose 环境需先在 mcpbridges/default.yaml 注册 DNS 服务来源，且 2.2.3 的
   McpBridge 不支持 `protocol: redis`（报 `invalid protocol:redis`），用 `tcp` 即可：

```yaml
# mcpbridges/default.yaml — redis 服务来源
registries:
  - name: devops-redis
    type: dns
    protocol: tcp            # redis 协议 2.2.3 不认，tcp 即可
    domain: redis.devops.local   # 必须是带点的完整域名；对应 compose 里 redis 的网络别名
    port: 6379
```

```yaml
# cluster-key-rate-limit.yaml
matchRules:
  - ingress: [devops-system, devops-system-gray-route]
    config:
      redis:
        service_name: devops-redis.dns   # dns 服务来源的引用格式：<registry名>.dns
        service_port: 6379
        password: "123456"               # 生产从密管注入，不提交明文
      rule_items:
        - limit_by_per_ip: from-remote-addr
          limit_keys:
            - key: 0.0.0.0/0
              query_per_second: 100
      rejected_code: 429
```

`redis.devops.local` 别名在 docker-compose-dev.yml 的 redis 服务上声明
（`networks.default.aliases`），Docker 内嵌 DNS 由此在 compose 网络内解析到 redis
容器。

调整维度：按 Header/Cookie 限流改 `limit_by_per_header` / `limit_by_per_cookie`；
按不同 key 组设置差异阈值在 `limit_keys` 里加条目。

### 6.3 GeoIP（geo-ip-1.0.0.yaml）

AUTHN 阶段解析客户端 IP 归属地，URL 转义后注入 `X-Higress-Geo-Country/-Province/
-City/-Isp` 请求头；内网 IP 时值为「内网IP」。消费方是 devops-system 登录审计：
`router/operations.go` 的 `withClientMeta` 中间件反转义这四个头 + gin ClientIP，
随登录写入 `t_system_login_log`（geo 字段），IP 本身另有 X-Forwarded-For 链，
不依赖本插件。取源 `ip_source_type: origin-source`（下行连接真实来源，客户端
直连 8080 无前置 LB）；若前置再加一层代理，改 `header` + 对应头并确保该层写入。

审计读取侧为管理端 `/api/v1/login-logs`（分页查询/删除/清空，权限
`system:loginlog:list|delete|clean`，菜单种子见
`internal/dbinit/sql/data/t_system_menu.sql`）；落库前 dao 层按
列宽截断 UA/Geo 头，超长头不会丢审计行。

### 6.4 JWT 认证（已落地：`wasmplugins/jwt-auth-2.0.0.yaml`）

网关侧第一道校验，由 `jwt-auth` 插件完成：

1. 从 `Authorization: Bearer <token>` 读取 JWT，校验签名、`issuer=devops`、`exp`、`iat`；
2. `claims_to_headers` 把 `uid` / `username` / `tid` / `tcode` Claims 转发为内部 Header，
   `override=true` 覆盖客户端同名 Header，防止伪造租户身份；
3. 验签公钥**内联**在插件 yaml 的 `jwks` 字段中（RS256 公钥，插件文件仅含公钥可入库）。
   ⚠️ Higress jwt-auth **没有 `jwksFromUrl` 字段**，故网关**不会**从
   `/.well-known/jwks.json` 拉取——那个端点只服务「其他服务自验签」与人工排查。

密钥策略：system 持 RS256 私钥签发，Higress 只配公钥 JWKS。
**JWT 私钥不得写入本目录公开配置文件。**换密钥后必须重新导出插件里的公钥
（`openssl rsa -noout -modulus` 导出 n，`e=65537`），否则全量 401。

网关认证通过后，`devops-system` 的 JWT 中间件仍需保留：system 还负责 Redis 会话
吊销检查、租户状态检查与租户上下文注入，不能仅凭 `X-Tenant-ID` 信任请求。

插件配置里的三个坑与调试判据见「7.3 第二层：JWT 验签」。

---

## 七、流量入口统一管理（三层收口）

### 7.1 为什么需要收口

只要 `devops-system:9999` 在容器网络内可达，任何一个被攻陷或误配的容器都能直连后端。
此时 **JWT 鉴权仍然有效**（它在应用内部），但**网关层能力全部失效**：

| 失效的能力 | 后果 |
| --- | --- |
| `cluster-key-rate-limit` | 限流形同虚设，可直接打后端 |
| `geo-ip` 注头 | 登录审计 `X-Higress-Geo-*` 四字段全空，审计链断裂且无法区分正常与旁路请求 |
| `canary` 灰度 | 可绕过灰度直接打生产实例 |
| `cors` | 跨域策略失效 |
| 访问日志 | 入口日志缺失，事后无法审计 |

### 7.2 三层模型

| 层 | 机制 | 强制力 | 生效形态 |
| --- | --- | --- | --- |
| 第一层 网络 | K8s NetworkPolicy | **强制** | K8s |
| 第一层 网络 | compose `edge`/`infra` 双网络 + `internal: true` | 部分强制（挡基础设施侧旁路） | Docker |
| 第二层 网关 | `jwt-auth` 插件验签 JWT（`jwt-auth-2.0.0.yaml`） | 需配合第三层 | Docker / K8s |
| 第三层 应用 | `devops-infra/security/gateway` 中间件独立校验 JWT，失败 401 | **强制** | Docker / K8s |

**Docker 形态**：compose 里 `devops-system` 只 `expose: "9999"` 无 `ports`，宿主机层面
已无法旁路；再叠加 `edge`/`infra` 网络隔离，MySQL/Redis/Nacos/etcd 容器不在业务网络内。
真正的强制手段是第三层守卫——因为 compose 无法对单个端口做「只允许网关访问」的 ACL。

**K8s 形态**：第一层 NetworkPolicy 真正闭合（应用层守卫退化为 defense-in-depth）。
`deploy/k8s/networkpolicy.yaml` 中 `allow-gateway-to-system` 让 9999 只接受来自
`higress-system` namespace 的 `app=higress` Pod。

### 7.3 第二层：JWT 验签（`jwt-auth` 插件）

插件配置见 `wasmplugins/jwt-auth-2.0.0.yaml`。要点：

- `global_auth: false`（**必须**）：`true` 会让插件对网关内**所有**路由全局生效，
  包括登录接口 → 用户还没有 token 却要求携带 → **无人能登录，系统自锁**。
- `matchRules.ingress` = `[devops-system, devops-system-gray-route]`，
  **刻意不含** `devops-system-public`。
- `keep_token: true`：转发到后端时保留 JWT。后端仍要校验 Redis 会话吊销、
  租户状态与租户上下文，不能仅凭网关注入的 `X-Tenant-ID` 信任租户身份。
- 认证配置（`consumers` / `jwks`）放**实例级 `defaultConfig`**，规则内只写
  鉴权 `config: { allow: [...] }`——官方要求同一规则内认证与鉴权不可共存。

#### Higress 2.2.3 all-in-one 的三个坑（均不报错误日志）

1. **`url` 不能用 `oci://`**。file 后端 + all-in-one 下 controller 不拉镜像，
   插件静默不加载。须用 `http://localhost:8002/plugins/jwt-auth/2.0.0/plugin.wasm`
   （与本目录其余插件一致）。
2. **`matchRules.config` 内联 `consumers` 数组 → 整个插件被静默跳过**。
   pilot 解析失败不报错，请求直接穿透到后端。写 `config: {}` 也不行——
   那会整体替换 `defaultConfig`，真 token 也会被 `403 Access Denied`。
3. **`metadata.name` 不能叫 `jwt-auth`**。pilot 会走 Istio 原生分支
   （日志出现 `--tokenManagerPlugin=""` 与 `JwtRule: ""`），而 all-in-one 2.2.3
   既不识别 `security.istio.io` 资源、envoy 也无 `jwt_authn` filter，一律不加载。
   改名为 `jwt-auth-2.0.0` 即可（wasm 文件路径不受影响）。

#### 为什么匿名端点必须单独一个 ingress 文件

插件按 **Ingress 资源**整体挂载，`matchRules` 只有 `ingress` / `domain` / `service`，
**没有 path / prefix**。而主 ingress 的 `/system` 是 Prefix 前缀，本就覆盖登录路径——
若登录写在同一文件里，插件一挂上去就把登录锁死。故 `login` / `refresh` / `jwks.json`
拆到 `devops-system-public.yaml`，靠 Higress **最长前缀匹配**让这三个更精确的路径
命中它，从而绕开插件。

实测（同一 host `api.devops.local`）：`/system/api/v1/users` 无 token →
`Jwt is missing`（被拦）；`/system/api/v1/auth/login` → 200（豁免成功）。

#### 插件是否真的加载了

**不要看 config_dump 的 ECDS**——all-in-one 下它始终为空，连生效的 cors 也一样。可靠判据：

```bash
docker compose -f deploy/docker-compose-dev.yml exec higress \
  sh -c 'grep -c "fetching image plugins/jwt-auth" /var/log/higress/gateway.log'
```

或看无 token 请求的响应形态：**纯文本** `Jwt is missing` = 插件生效；
信封格式 `{"code":401,"msg":"未登录或会话已过期"}` = 应用层，插件没生效。

### 7.4 第三层：应用守卫

中间件位于 `devops-infra/security/gateway`，要求请求携带**可独立校验通过**的
Bearer JWT（签名 + Redis 会话态），失败统一返回 401 且不区分「缺凭据」与
「凭据无效」（防探测）。挂载位置在 `devops-infra/http/server.go`：运维端点之后、
业务路由之前，否则 `/healthz` `/readyz` `/metrics` 会被拦。

**为什么网关验过了应用层还要再校验一次**：

- jwt-auth 以 Ingress 资源为粒度挂载，无法表达「仅这些路径需鉴权」的细粒度策略，
  本层承担细粒度收口；
- compose 形态没有 NetworkPolicy，绕过网关时不经过任何插件；
- K8s 形态下本层退化为 defense-in-depth，仍有价值。

配置项（`deploy/config/system-prod.yaml`）：

```yaml
gateway:
  require_jwt: true
  exempt_paths: []   # /healthz /readyz /metrics、login/refresh/jwks、apidoc 已由代码放行
```

**`require_jwt: false` 时守卫直通**，未启用环境零改动。dev 刻意关闭：业务服务以
`go run` 跑在宿主机 `127.0.0.1:9999`，本地 curl 直连调试会被 401 显著拖慢。

**fail-closed 不变式**：`require_jwt: true` 但未装配 JWT 校验器时**拒绝**而非放行，
否则等于全站放行、收口形同虚设且无任何告警。

放行名单中的匿名端点路径与 huma 注册同源（`devops-system/internal/router/operations.go`
导出的 `AuthLoginPath` / `AuthRefreshPath` / `JWKSPath`），避免与
`devops-system-public.yaml` 各写一份字符串而悄悄漂移——漂移后果是登录被锁死。

### 7.5 验证命令

```bash
# 1) 确认插件已加载（输出应为 1，非 0）
docker compose -f deploy/docker-compose-dev.yml exec higress \
  sh -c 'grep -c "fetching image plugins/jwt-auth" /var/log/higress/gateway.log'

# 2) 匿名端点可访问（期望 200）
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: api.devops.local' \
  http://localhost:8080/system/api/v1/auth/login

# 3) 业务接口无 token：应 401（插件与应用层双重收口）
curl -s -H 'Host: api.devops.local' \
  http://localhost:8080/system/api/v1/users

# 4) 登录拿 token
TOKEN=$(curl -s -X POST -H 'Host: api.devops.local' -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<pwd>"}' \
  http://localhost:8080/system/api/v1/auth/login | jq -r .data.access_token)

# 5) 带 token：应 200
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: api.devops.local' \
  -H "Authorization: Bearer $TOKEN" http://localhost:8080/system/api/v1/users

# 6) 直连容器绕过网关：应 401（第三层守卫生效）
docker compose -f deploy/docker-compose.yml exec devops-system \
  wget -qO- --header='Host: x' http://127.0.0.1:9999/system/api/v1/users

# 7) K8s 确认 NetworkPolicy 真的被 CNI 执行（见 networkpolicy.yaml 顶部警告）
kubectl -n devops get networkpolicy
```

### 7.6 常见误区

- **只做 compose 网络隔离就以为收口了**：Docker 无端口级 ACL，同 edge 网络的容器
  仍可旁路。必须叠加第三层。
- **沿用静态共享密钥当流量入口凭据**（已废弃的 `X-Gateway-Token`）：不表达身份、
  无过期、无吊销，明文散落在 ingress 与配置中，泄露即无限重放且审计无法归因。
- **只改主 ingress 忘了 canary / gray 路由**：灰度流量全量 403。
- **secret 配空**：`Enforce` 会直接直通（设计如此，避免误伤），但意味着收口静默失效。
  上生产前务必 grep 确认 token 非空。
- **NetworkPolicy apply 后不测**：kind/k3s 默认 kindnet 不支持 NetworkPolicy，
  apply 无报错但完全无效。

---

## 八、可观测性

外部栈（dev compose / 生产均适用）：**Jaeger（trace）+ Prometheus（指标）+
Grafana（看板）**，配置在 `deploy/prometheus/` 与 `deploy/grafana/`。

- **抓取网关指标**：Envoy admin 端口 15020 输出合并指标
  （`higress-config` 的 `enablePrometheusMerge: true`），Prometheus 抓取任务：

  ```yaml
  - job_name: higress-gateway
    metrics_path: /stats/prometheus
    static_configs:
      - targets: ["higress:15020"]
  ```

- **不要开启全量指标**：`higress-config` 中
  `proxyStatsMatcher.inclusionRegexps: [.*]` 会让 Envoy 为每个操作生成统计，配合
  抓取会造成严重 CPU 消耗（实测可把整个 Docker 引擎压死）。除非排障需要，保持删除。
- **all-in-one 内置可观测组件**：镜像自带 prometheus/loki/promtail/grafana，与
  外部栈重复且是 dev 环境 CPU 高的主因之一。dev 用外部栈时应当禁用（控制台关闭
  内置仪表盘：`dashboard.builtin: false`；确认方式：
  `docker exec <higress容器> supervisorctl status`，若仍列出 o11y 进程，用空文件
  覆盖对应 `/etc/supervisor/` 程序配置）。禁用后网关访问日志只能从
  `docker logs` 查看，若需 Loki 日志查询需在外部栈补建。

---

## 九、Docker 生产级部署方案

### 9.1 为什么不能用 all-in-one 上生产

all-in-one 把 pilot/controller、gateway、console、内置可观测组件塞在一个容器里，
supervisord 拉起所有进程：无法独立扩缩容（网关是无状态热点组件，controller 不是）、
任一组件 OOM 整容器重启、无法按组件设置资源上限与滚动升级、内置 o11y 与生产监控
体系冲突。生产（docker 形态）应**组件分离 + Nacos 作配置后端**：

```
┌────────────────────────────────────────────────┐
│ docker compose (单机)                            │
│                                                  │
│  higress-gateway ×N  ── 无状态，可 scale，挂载    │
│        ▲ xDS                最小配置              │
│  higress-controller ×1 ── 连 Nacos 读写路由/插件   │
│        ▲ REST               配置（配置后端）       │
│  higress-console ×1    ── 管理界面，仅内网可达     │
│                                                  │
│  nacos(集群/独立)  redis  mysql  业务服务           │
└────────────────────────────────────────────────┘
```

### 9.2 生产 compose 模板

> 组件镜像与启动参数以官方 **higress-standalone**
> （github.com/higress-group/higress-standalone）仓库的 docker-compose 为准——
> 它是 Higress 官方维护的非 K8s 部署方案，与本节结构一致；撰写本文时网络受限
> 未能逐一核对镜像 tag 与环境变量，落地前请对照该仓库校准标注 ⚠ 的部分。

```yaml
# deploy/docker-compose-gateway.yml —— 生产网关栈（业务栈见 docker-compose.yml）
name: devops-gateway

services:
  higress-controller:
    image: higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/controller:2.2.3   # ⚠ tag 对照 higress-standalone
    restart: unless-stopped
    environment:
      # ⚠ 以下连接参数以 higress-standalone 官方 compose 为准
      NACOS_ADDRESS: nacos:8848          # 配置后端：路由/插件/McpBridge 存 Nacos
      NACOS_NAMESPACE: public
      NACOS_GROUP: higress-config         # Higress 自身配置分组（非业务分组）
    volumes:
      - gateway-controller-data:/data
    deploy:
      resources:
        limits: { cpus: "1", memory: 1G }   # controller 处理配置推送，1C1G 起步
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:15014/ready"]   # ⚠ 端口以官方为准
      interval: 10s
      timeout: 5s
      retries: 12
    logging: { driver: json-file, options: { max-size: "50m", max-file: "5" } }

  higress-gateway:
    image: higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/gateway:2.2.3      # ⚠ 同上
    restart: unless-stopped
    depends_on:
      higress-controller:
        condition: service_healthy
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - gateway-data:/data
    deploy:
      replicas: 1            # 单机 compose 无法跨节点；多节点用 K8s
      resources:
        limits: { cpus: "2", memory: 2G }
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:15020/ready"]   # ⚠ 端口以官方为准
      interval: 10s
      timeout: 5s
      retries: 12
    logging: { driver: json-file, options: { max-size: "50m", max-file: "5" } }

  higress-console:
    image: higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/console:2.2.3       # ⚠ 同上
    restart: unless-stopped
    depends_on:
      - higress-controller
    ports:
      - "127.0.0.1:8001:8001"   # 只绑回环！经 SSH 隧道/内网反代访问，不上公网
    logging: { driver: json-file, options: { max-size: "20m", max-file: "3" } }

volumes:
  gateway-controller-data:
  gateway-data:
```

### 9.3 生产化要点清单

**配置管理**
- 生产用 **Nacos 作为 Higress 配置后端**（controller 直连 Nacos），不再使用文件
  后端；本目录 yaml 作为声明式真源，通过控制台/API 导入或 CI 同步到 Nacos，
  修改后由 controller 秒级推送 gateway。
- McpBridge 的 `domain` 改为生产 Nacos 地址；限流插件的 `redis.service_name`
  改为生产 Redis 地址。

**安全**
- console 端口只绑 `127.0.0.1` 或内网，经堡垒/反代访问；首次初始化后立即改掉
  默认管理员口令。
- Nacos 生产开启鉴权（`NACOS_AUTH_ENABLE=true` + 密钥），当前 dev 的关闭状态
  仅限本地。
- 网关层配 `jwt-auth`（见 6.4）；TLS 证书挂载至 8443，HTTP 80 只做 301 跳转。
- Redis/MySQL 等密码一律经 `.env`（不提交）或密管注入，compose 中用 `${VAR}`。

**资源与稳定性**
- 每个组件设 `deploy.resources.limits`（参考值见模板），防止单组件拖垮宿主机。
- 全部 `restart: unless-stopped` + healthcheck；日志 `json-file` 轮转防磁盘打爆。
- 镜像 tag 固定到具体版本，禁止 `latest`；升级前 `docker compose pull` +
  逐组件 `up -d <svc>` 滚动重启，先 gateway 后 controller。

**备份与恢复**
- Nacos 数据卷每日备份（配置后端 = 所有路由/插件配置都在其中）。
- 本目录 git 提交记录每次配置变更，作为回滚依据。
- 网关数据卷不含业务状态，可重建；重建后从 Nacos 重新拉取配置即可。

**监控**
- 复用现有 Prometheus/Grafana 栈：抓 `higress-gateway:15020/stats/prometheus`
  （指标）与 controller 指标；Grafana 导入 Envoy/Istio 面板观察 4xx/5xx、P99
  延迟、连接数，配合 Jaeger trace 定位慢请求。
- 告警项：gateway 健康检查失败、5xx 比例、限流 429 突增、controller 与 Nacos
  连接断开。

---

## 十、Kubernetes 部署（正式生产推荐）

规模化、高可用、滚动升级需求强烈时迁移 K8s，见 `deploy/k8s/devops.yaml`：

```bash
helm repo add higress.io https://higress.cn/helm-charts
helm install higress -n higress-system higress.io/higress --create-namespace
kubectl apply -f deploy/higress/mcpbridges/default.yaml
kubectl apply -f deploy/higress/ingresses
kubectl apply -f deploy/higress/wasmplugins
kubectl apply -f deploy/k8s/devops.yaml
# 网络收口：让 devops-system:9999 只接受来自 higress-system 的流量。
# 必须先确认 CNI 支持 NetworkPolicy（Calico/Cilium），kindnet 不支持。
kubectl apply -f deploy/k8s/networkpolicy.yaml
```

K8s 形态下第一层收口是**真正强制**的，`devops-system` 被 NetworkPolicy 挡住后
任何旁路都会连接超时（详见「7.2 三层模型」）。迁移时注意三处必须同步改：
- `deploy/higress/wasmplugins/jwt-auth-2.0.0.yaml` 里的内联公钥须与
  `devops-system` 的 `jwt.signing_key` 对应（`kid` / `iss` 也要一致）。
  wasm-go 实现也可改用 `remote_jwks` 指向 `/.well-known/jwks.json` 自动跟随轮换，
  但要求该服务先在 McpBridge 中可达，且拉取失败窗口内请求会被 fail-close 拒绝；
- `deploy/k8s/devops.yaml` 中 Secret 的 `gateway.require_jwt` 置 true；
- `allow-observe-and-probe` 里的 `ipBlock.cidr` 要改成实际集群节点网段，
  放 `0.0.0.0/0` 等于没放；网段写错会导致 kubelet 探针失败、Pod 被反复重启。

迁移时需要调整的地址类配置：
- `mcpbridges/default.yaml` 的 `domain` 改为实际 Nacos Service
  （如 `nacos.middleware.svc.cluster.local`），确认 8848/9848 可达；
- 限流插件 `redis.service_name` 改为实际 Redis Service，同步修改密码与阈值；
- Ingress 的 host 换成真实域名，TLS 证书走 cert-manager 或手动 Secret。

---

## 附：常用命令速查

```bash
# dev：启动 / 同步配置 / 停本地服务
sh deploy/dev.sh          # 容器 + 自动灌配置 + 拉起本地服务
sh deploy/dev.sh sync     # 改完 higress yaml 后同步到命名卷
sh deploy/dev.sh app      # 只启动本地业务服务

# 路由验证
curl -i -H 'Host: api.devops.local' http://localhost:8080/healthz

# 容器内排查
docker compose -f deploy/docker-compose-dev.yml exec higress supervisorctl status
docker compose -f deploy/docker-compose-dev.yml exec higress cat /data/mcpbridges/default.yaml

# 控制台
# http://localhost:8001  （dev）
```
