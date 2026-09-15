# Nacos + Higress 引入方案设计

> 适用范围：`devOps` 多租户用户权限系统（go.work 三模块：devops-infra / devops-system / devops-id）
> 状态：**待评审**（本文件仅方案，不含代码改动）
> 版本：v1.1 · 2026-09-15 11:57:07 (CST)

---

## 0. 结论摘要

| 维度 | 结论 |
| --- | --- |
| 可行性 | 可行，且改造面可控（新增 4 个文件 + 修改 12 个文件，无业务层改动） |
| 引入顺序 | **配置中心 → 注册中心 → 网关**，三段独立开关，每段可单独回退 |
| 最大收益 | 1) 配置集中与热更新；2) 去掉 `idclient.endpoint` 硬编码，devops-id 可弹性扩缩；3) 南北向流量统一收口（CORS/限流/灰度/租户路由） |
| 最大风险 | 配置热更新与现有「包级单例 Init」架构冲突；优雅退出时序（不反注册就停机会导致 502） |
| 建议范围 | 一期只做 **配置中心 + 注册中心**，网关随二期接入；JWT 鉴权**不要**上移到网关（只做可选粗粒度拦截） |
| 不做的事 | etcd 不替换（snowflake 槽位仍需 lease 语义）；devops-id 的 gRPC **不过** Higress |

---

## 1. 现状与目标架构

### 1.1 现状

```
                        ┌─────────────────────────────┐
   Client ─────────────▶│ devops-system :9999 (Gin)   │
   (直连 / K8s Service) │  JWT → Tenant → Casbin       │
                        └──────────────┬──────────────┘
                                       │ gRPC（硬编码 endpoint）
                                       │ idclient.endpoint:
                                       │   "devops-id.devops.svc.cluster.local:9090"
                                       ▼
                        ┌─────────────────────────────┐
                        │ devops-id :9090 (gRPC)      │
                        │  segment / snowflake         │
                        └──────────┬──────────────────┘
                                   ▼
                    MySQL 8 · Redis · etcd(仅 snowflake)

配置：viper 读 config/config.yaml + config/{env}.yaml（APP_ENV 选择），WatchConfig 热重载
部署：deploy/docker-compose.yml（单机）· deploy/k8s/devops.yaml（K8s）
```

关键事实（影响改造方案）：

1. **配置是启动时快照**：`infracfg.Init()` → `LoadInto(&cfg)` → 各包 `Init()` 建连接池。viper 的 `WatchConfig` 只重载 viper 内部值，**不会**重建 db / redis / authz / idclient。
2. **服务发现等于零**：devops-system → devops-id 是静态地址，K8s 环境靠 Service DNS，docker-compose 环境靠容器名。
3. **鉴权全在应用内**：JWT + Tenant + Casbin 三层中间件，权限数据在 MySQL（`casbin_rule`），网关拿不到。
4. **会话在 Redis**：devops-system 无状态，可任意水平扩。
5. **etcd 仅用于 snowflake workerID 槽位抢占**（lease 保活 + TTL 释放）。

### 1.2 目标架构

```
   Client
     │ HTTP/HTTPS
     ▼
┌──────────────────────────────────────────────────────────┐
│  Higress Gateway (Envoy 内核)                             │
│   南北向收口：路由 / CORS / 限流 / 访问日志 / 灰度          │
│   可选：jwt-auth Wasm 插件（粗粒度，仅签名+exp 校验）       │
│   服务来源：McpBridge → Nacos (nacos2 / gRPC 9848)         │
└───────────────────────────┬──────────────────────────────┘
                            │ devops-system.DEVOPS-GROUP.public.nacos
                            ▼
┌──────────────────────────────────────────────────────────┐
│  devops-system :9999            [Nacos 注册 + 配置]        │
│   JWT → Tenant → Casbin（保持不变）                        │
└───────────────────────────┬──────────────────────────────┘
                            │ gRPC，nacos://devops-id
                            │ （自定义 resolver + round_robin，不过网关）
                            ▼
┌──────────────────────────────────────────────────────────┐
│  devops-id :9090                [Nacos 注册 + 配置]        │
└───────────────────────────┬──────────────────────────────┘
                            ▼
              MySQL 8 · Redis · etcd(仅 snowflake)

┌──────────────────────────────────────────────────────────┐
│  Nacos Server 2.x  :8848(HTTP) / :9848(gRPC client)       │
│   注册中心：临时实例 + SDK 心跳                            │
│   配置中心：dataId 分级 + 长轮询监听                        │
└──────────────────────────────────────────────────────────┘
```

---

## 2. 关键设计决策

### 决策 1：devops-id 的 gRPC 走不走 Higress？—— **不走**

| 方案 | 评价 |
| --- | --- |
| 走 Higress | 多一跳 Envoy（+0.3~1ms）、需配置 gRPC 路由与 HTTP/2、Higress 成为内部调用的单点、收益为零 |
| **Nacos 服务发现直连** ✅ | 客户端负载均衡（round_robin），无中心瓶颈，与现有 `idclient` 预取缓冲天然契合 |

东西向内部流量走 Nacos 直连，南北向 HTTP 才走 Higress。这是微服务网关的常规边界划分。

### 决策 2：Nacos 配置与本地文件的关系 —— **bootstrap 本地化，业务配置 Nacos 化**

不推倒现有 viper 机制，而是**在 viper 之上加一层合并**：

```
优先级（低 ────────────────────────────▶ 高）

① config/config.yaml        本地基础配置（全量默认值，同时充当"配置文档"）
② config/{APP_ENV}.yaml     本地环境覆盖（现有机制，保持不变）
③ Nacos  {service}.yaml     主配置
④ Nacos  {service}-{env}.yaml  环境覆盖（最终生效）
```

带来的三个好处：

- **Nacos 挂了照样能起**：①②③ 已足够，Nacos 只是增量覆盖（可通过 `nacos.config.required` 控制是否强依赖）。
- **开关一键回退**：`nacos.enabled: false` 时 ③④ 完全跳过，行为与今天 100% 一致。
- **迁移可渐进**：先只把 1~2 个非敏感段（如 `log`）搬到 Nacos，稳定后再整体搬迁。

`config/config.yaml` 中新增的 **bootstrap 段**（Nacos 地址等）只能来自本地文件或环境变量，不能来自 Nacos（鸡生蛋问题）。

### 决策 3：配置热更新的边界 —— **分级，默认只热更 L1**

这是整个方案**最容易踩坑**的地方。现有架构是「启动时建连接池，之后不再动」，无脑全量重载会：重建连接池导致在途事务中断、改 JWT 密钥导致所有人掉线、改 `tenant.mode` 导致租户路由错乱。

因此把配置项分三级：

| 级别 | 语义 | 配置项 | 变更时行为 |
| --- | --- | --- | --- |
| **L0 静态** | 需重启 / 滚动发布 | `mysql.*`、`redis.*`、`jwt.*`、`tenant.mode`、`server.port`、`grpc.listen`、`idgen.mode` | 检测到 diff → **只记 WARN 日志 + 指标**，不重载，提示"需重启生效" |
| **L1 可热更** | 运行时安全切换 | `log.level`、`log.console`、`server.cors.*`、`server.trusted_proxies`、`idclient.batch` / `low_water` / `*_timeout`、`tenant.default_code`、`tenant.max_pools` | 走注册的回调原子生效 |
| **L2 特殊** | 需配套逻辑 | `jwt.signing_key` | 需双密钥轮转（新旧都验签，一个 access 周期后切新），列入 Phase 3 |

实现要点：

- `nacosx.OnConfigChange(fn)` 注册回调，`fn(oldCfg, newCfg any)`。
- 内置一个 `diffLogger`：L0 字段变化 → `slog.Warn("配置变更需重启生效", "fields", [...])`。
- 一期只需把 `log.level` 接到 `logger.SetLevel()`，`idclient` 参数改为 `atomic.Value` 读取即可，工作量很小。

### 决策 4：JWT 鉴权放网关还是应用？—— **保留在应用，网关仅做可选粗粒度**

| 阶段 | 做法 | 理由 |
| --- | --- | --- |
| **Phase 2（推荐）** | 网关**不校验** JWT；只做路由 + CORS + 限流 + 日志。应用侧三层中间件原样不动 | Casbin 权限数据在应用侧，网关无 RBAC 数据；HS256 密钥共享给网关扩大泄露面；改造最小，风险最低 |
| **Phase 3（可选）** | 启用 Higress `jwt-auth` Wasm 插件做**粗粒度**拦截（签名 + exp 校验，公开路由配白名单），应用侧**保留完整鉴权** | 纵深防御：挡掉海量无效/过期 token，减少后端压力。注意是"叠加"不是"替代" |

按现有路由，网关侧白名单（若启用 jwt-auth）应为：

```
公开：/healthz、/api/v1/auth/login、/api/v1/auth/refresh
其余：/api/v1/* 全部要求有效 JWT
```

⚠️ **真实坑位**：Higress `jwt-auth` 只接受 **JWKS 格式**，HS256 的 `k` 字段是**密钥原始字节的 base64url 编码**，不是密钥字符串本身。本项目 `jwt.signing_key` 是以字符串形式直接作 HMAC key（`jwt/v5` 用 `[]byte(key)`），迁移时需要：

```bash
# 生成 JWKS 的 k 值（注意是 signing_key 字节的 base64url，无 padding）
printf '%s' "$SIGNING_KEY" | base64 | tr '+/' '-_' | tr -d '=\n'
```

且密钥一旦放进 Higress 配置，就多了一处需要同步轮换的地方——这也是 Phase 2 不启用的核心原因。

### 决策 5：etcd 保留还是替换？—— **保留**

Nacos 没有 lease / 事务型槽位抢占语义（虽有分布式锁，但语义和代价都不合适）。snowflake 的 workerID 分配需要「抢占 + 保活 + 宕机 TTL 自动释放」，etcd 的 lease 是最贴切的原语。

- `idgen.mode=segment`：**完全不需要 etcd**，Nacos 引入后可彻底下线 etcd 依赖。
- `idgen.mode=snowflake`：etcd 继续保留，与 Nacos 各司其职（Nacos 管注册/配置，etcd 管槽位）。

### 决策 6：CORS 上移到网关 —— **上移后必须关掉应用层**

Higress 配 CORS 后，若 devops-system 仍配 `server.cors.allow_origins`，响应会出现**两个** `Access-Control-Allow-Origin` 头，浏览器直接报错。

动作：网关接入时，`deploy/config/system-prod.yaml` 的 `server.cors.allow_origins` 置为 `[]`（httpx 已有逻辑：空则不挂载，天然支持）。

### 决策 7：`trusted_proxies` 必须加网关网段

Higress（Envoy）会追加 `X-Forwarded-For`。devops-system 若 `server.trusted_proxies` 不含网关地址，`c.ClientIP()` 会拿到网关 IP，导致：登录日志/审计 IP 全错、未来基于 IP 的限流失效。

动作：

```yaml
server:
  trusted_proxies:
    - 10.0.0.0/8        # K8s Pod/Service CIDR（按实际调整）
    - 172.16.0.0/12     # docker bridge（本地 compose 场景）
```

> 注：当前 K8s 清单里已配 `["10.0.0.0/8"]`，需按实际 Higress Pod 网段复核。

### 决策 8：健康检查三轨并存，明确最坏摘除时间

| 层 | 机制 | 检测延迟 | 说明 |
| --- | --- | --- | --- |
| Nacos | 临时实例（ephemeral）+ SDK 心跳 | 心跳 5s / 15s 标记不健康 / 30s 剔除 | 进程 `kill -9` 也能兜住 |
| K8s | `httpGet /healthz`（liveness + readiness） | periodSeconds 10~15s | Pod 级保护，保留 |
| Higress | 可选：对 Nacos 实例的主动健康检查 | 取决于配置 | 建议开启，指向 `/healthz` |

现状 `/healthz` 只返回 `{"status":"ok"}`，不探依赖。建议**新增 `/readyz`**（探 MySQL + Redis + devops-id 连通性），让 K8s readiness 能反映真实可用性——但**不要**把 liveness 指向 `/readyz`，否则 DB 抖动会引发全量重启雪崩。

### 决策 9：优雅退出时序必须改 —— **先反注册，再等待引流窗口**

这是引入注册中心后最经典的故障源。现有退出链：

```
收到 SIGTERM → httpx.Run 返回（10s 排空在途）→ 关 resolver → authz → idclient → store → db
```

问题：`httpx` 停止监听后，Nacos 里的实例仍健康，Higress/Envoy 需要 **Nacos 推送（秒级）+ Envoy xDS 生效（秒级）** 才会摘除，这段窗口内的新请求会打到已停止监听的端口 → **502**。

改造后的时序：

```
收到 SIGTERM
  → ① 置本地 /healthz 为 not-ready（让 K8s/Higress 主动探测先失败）
  → ② Nacos DeregisterInstance（立即从注册中心移除）
  → ③ 等待"引流窗口" NACOS_DRAIN_WAIT（默认 5s，可配）
       等待期间 HTTP 继续服务，消化已建立连接与最后一波请求
  → ④ httpx 优雅关闭（10s 排空）
  → ⑤ 关 resolver → authz → idclient → store → db
  → ⑥ Nacos client Close（最后一次心跳确保发出）
```

`NACOS_DRAIN_WAIT` 建议取值 ≥ Nacos 推送间隔 + Envoy xDS 生效时间，5s 是经验值，本地可设 2s。

### 决策 10：注册 IP 的获取策略

nacos-sdk-go v2 的 `RegisterInstanceParam` 需显式传 `Ip`。注册成 `127.0.0.1` 或不可达网卡 IP 是高频故障。

优先级：

1. `nacos.ip` 显式配置（最高）
2. 环境变量 `POD_IP`（K8s downward API 注入，最可靠）
3. 环境变量 `INSTANCE_IP`（docker-compose 场景）
4. 自动探测：遍历非 loopback、非 docker0 的私有 IPv4（写 WARN 日志提示确认）

K8s 侧配合：

```yaml
env:
  - name: POD_IP
    valueFrom:
      fieldRef:
        fieldPath: status.podIP
```

### 决策 11：Nacos 里的敏感配置 —— **必须开鉴权 + 敏感项不入库**

Nacos 2.2+ 默认要求鉴权，`nacos/nacos-server` 镜像需显式配置：

```bash
NACOS_AUTH_TOKEN=<Base64 编码、原始串 ≥32 字符的随机串>   # 必填，否则启动失败或鉴权被绕过
NACOS_AUTH_IDENTITY_KEY=serverIdentity
NACOS_AUTH_IDENTITY_VALUE=security
```

生成：

```bash
openssl rand -base64 32
```

即便开了鉴权，仍建议：**DB 密码 / Redis 密码 / JWT 密钥走 K8s Secret → 环境变量注入**，Nacos 只放非敏感配置。原因是 Nacos 控制台/Web API 的可见范围通常比 K8s Secret 大得多。

---

## 3. Nacos 资源规划

### 3.1 命名空间与分组

| 项 | 值 | 说明 |
| --- | --- | --- |
| Namespace ID | `devops`（prod）/ `devops-test`（test） | 环境间天然隔离，避免 test 实例被 prod 网关发现 |
| Group | `DEVOPS_GROUP` | 与默认 `DEFAULT_GROUP` 区分，便于多系统共用一套 Nacos |

> Higress `destination` 里 group 的下划线会被转成连字符：`DEVOPS_GROUP` → `DEVOPS-GROUP`。

### 3.2 配置 dataId 规划

| dataId | Group | 归属 | 内容 |
| --- | --- | --- | --- |
| `devops-system.yaml` | DEVOPS_GROUP | system | 全量公共配置（server/log/mysql/redis/jwt/tenant/idclient） |
| `devops-system-prod.yaml` | DEVOPS_GROUP | system | prod 差异覆盖（只写差异字段） |
| `devops-id.yaml` | DEVOPS_GROUP | id | 全量公共配置 |
| `devops-id-prod.yaml` | DEVOPS_GROUP | id | prod 差异覆盖 |
| `devops-common.yaml` | DEVOPS_GROUP | 共享 | 可选：日志格式、限流阈值等跨服务项（Phase 3 引入） |

格式统一 **YAML**（与现有配置格式一致，迁移成本最低，且可复用 viper 解析）。

### 3.3 服务名规划

| 服务 | serviceName | Group | 协议 | 端口 | metadata |
| --- | --- | --- | --- | --- | --- |
| devops-system | `devops-system` | DEVOPS_GROUP | HTTP | 9999 | `{"protocol":"http","version":"<git>"}` |
| devops-id | `devops-id` | DEVOPS_GROUP | gRPC | 9090 | `{"protocol":"grpc","version":"<git>"}` |

`metadata.protocol` 用于 gRPC resolver 过滤（避免误选到 HTTP 实例），也便于后续按版本灰度。

Higress 路由目标：

```
devops-system.DEVOPS-GROUP.devops.nacos
```

---

## 4. 代码改动清单

### 4.1 新增文件（devops-infra）

| 文件 | 职责 | 预估行数 |
| --- | --- | --- |
| `devops-infra/nacosx/client.go` | Nacos 客户端封装：`Init(cfg)` 建 config/naming 两个 client、`Close()`；未启用时返回 nil 且所有方法 no-op | ~120 |
| `devops-infra/nacosx/config.go` | 配置拉取 `Fetch(dataId)` → `[]byte`；监听 `Listen(dataIds, onRawChange)`；与 viper 合并的 `MergeInto(v)` | ~150 |
| `devops-infra/nacosx/registry.go` | `Register(serviceName, port, metadata)` / `Deregister()`；IP 决策（决策 10）；优雅退出语义 | ~120 |
| `devops-infra/nacosx/resolver.go` | gRPC `resolver.Builder` + `resolver.Resolver`：scheme `nacos`，`Subscribe` 回调 → `cc.UpdateState` | ~150 |

关键接口签名（示意，非最终实现）：

```go
package nacosx

// Config Nacos 接入配置（各服务 config.yaml 的 nacos 段）
type Config struct {
    Enabled   bool          `mapstructure:"enabled"`    // 总开关，false 时全包 no-op
    Endpoints []string      `mapstructure:"endpoints"`  // ["127.0.0.1:8848"]
    Namespace string        `mapstructure:"namespace"`  // devops
    Group     string        `mapstructure:"group"`      // DEVOPS_GROUP
    Username  string        `mapstructure:"username"`
    Password  string        `mapstructure:"password"`
    IP        string        `mapstructure:"ip"`         // 覆盖自动探测
    Timeout   time.Duration `mapstructure:"timeout"`

    ConfigCenter ConfigCenterConfig `mapstructure:"config"`    // data_ids/required
    Registry     RegistryConfig     `mapstructure:"registry"`  // service_name/port/weight
}

func Init(cfg Config) error                       // 建 client；Enabled=false 直接 return nil
func Close() error

// 注册中心
func Register(serviceName string, port int, meta map[string]string) error
func Deregister(serviceName string, port int) error

// 配置中心
func Fetch(dataID string) (string, error)
func Listen(dataID string, onChange func(content string)) error

// 优雅退出引流
func DrainWait() time.Duration
```

gRPC resolver 用法（对 `grpcx.Dial` 透明）：

```go
// 配置里写：idclient.endpoint: "nacos://devops-id"
// grpcx.Dial 检测到 nacos:// scheme → 自动注入 resolver + round_robin balancer
conn, err := grpcx.Dial(ctx, cfg.IDClient.Endpoint, cfg.IDClient.DialTimeout)
```

### 4.2 修改文件

| 文件 | 改动 | 风险 |
| --- | --- | --- |
| `devops-infra/go.mod` | `+ github.com/nacos-group/nacos-sdk-go/v2 v2.3.5+` | 低（新增依赖，注意 grpc/protobuf 版本对齐，现有 grpc 1.82 与 SDK 兼容） |
| `devops-infra/config/config.go` | 新增共享 `NacosConfig` 基础类型（可选，也可各服务自定义） | 低 |
| `devops-infra/config/viper.go` | `Init()` 增加第 3、4 阶段：Nacos 主配置 + 环境配置 `MergeConfig`；`Files()` 展示来源 | 中（需保证 Nacos 不可用时静默降级） |
| `devops-infra/grpcx/grpcx.go` | `Dial()` 识别 `nacos://` scheme，注入 resolver 与 `round_robin` | 中（保持旧签名与行为，向后兼容） |
| `devops-infra/httpx/httpx.go` | 新增 `/readyz`（依赖探活）；`/healthz` 支持被置为 not-ready（决策 9 步骤 ①） | 低 |
| `devops-system/internal/config/config.go` | `+ Nacos nacosx.Config`、`+ Service ServiceConfig{Name, Port, Metadata}` | 低 |
| `devops-system/internal/cmd/root.go` | `PersistentPreRunE` 增加 `nacosx.Init`；`PersistentPostRunE` 增加 `nacosx.Close` | 中（migrate 命令需跳过注册） |
| `devops-system/internal/cmd/server.go` | 注册实例；退出链改为 ①置 not-ready → ②Deregister → ③DrainWait → ④…原链 | **高**（时序敏感，需实测） |
| `devops-id/internal/config/config.go` | 同上加 Nacos 段 | 低 |
| `devops-id/internal/cmd/server.go` | 注册实例（gRPC）+ 反注册；`grpcx.Run` 需暴露 shutdown hook | 中 |
| `devops-system/config/*.yaml`、`devops-id/config/config.yaml` | 新增 `nacos:` 段（默认 `enabled: false`） | 低 |
| `deploy/config/system-prod.yaml`、`deploy/config/id-prod.yaml` | Nacos 段启用 + 敏感项保留在 Secret | 低 |
| `deploy/docker-compose.yml` | 加 `nacos`（standalone + MySQL）+ `higress`（all-in-one）；`depends_on` 健康检查 | 低 |
| `deploy/k8s/devops.yaml` | 加 `McpBridge`、`Ingress`、`POD_IP` 注入、Secret 增 Nacos 凭据、readiness 改 `/readyz` | 中 |
| 新增 `deploy/k8s/higress.yaml` | McpBridge + Ingress +（可选）WasmPlugin | 低 |
| `Makefile` | `+ nacos-up / higress-up / nacos-init`（初始化命名空间与配置） | 低 |
| `README.md` | 架构图、部署章节、配置项表格更新 | 低 |

**业务层（api / service / dao / model / middleware）零改动。** 这是本方案刻意保证的——所有变更都收敛在 infra 与 cmd 层。

### 4.3 依赖影响评估

`nacos-sdk-go/v2` 会带来：

- `google.golang.org/grpc`、`protobuf`（与现有版本共存，需 `go work sync` 后确认单一版本）
- `go.uber.org/zap`（devops-id 已因 etcd 间接引入）
- 约 40 个间接依赖，二进制体积 +8~12MB

建议在 `devops-infra` 单独引入，system/id 通过 go.work 共享，避免三处重复。

---

## 5. 具体实施步骤与代码逻辑

> 说明：本节为设计稿，代码块均为“示意代码”，用于表达实现顺序和关键判断，不能直接复制运行。落地时应以 `devops-infra` 现有包边界为准，`grpcx` 对应现仓库的 `devops-infra/grpc`，`httpx` 对应 `devops-infra/http`。

### 5.1 `nacosx/client.go`

职责：只负责“建立 / 关闭 Nacos 客户端”，不承担配置或注册语义。

```go
type ClientConfig struct {
    Enabled      bool
    Endpoints    []string
    NamespaceID  string
    Group        string
    Username     string
    Password     string
    Timeout      time.Duration
}

func Init(cfg ClientConfig) error {
    if !cfg.Enabled {
        return nil
    }
    if len(cfg.Endpoints) == 0 {
        return ErrNacosEndpointsEmpty
    }
    if cfg.NamespaceID == "" {
        return ErrNacosNamespaceEmpty
    }
    if cfg.Timeout <= 0 {
        cfg.Timeout = 5 * time.Second
    }

    // 1. 建立 config client
    // 2. 建立 naming client
    // 3. 保存到包级单例，供 config.go / registry.go 复用
    return nil
}

func Close() error {
    // 关闭 config client、naming client
    // 清空单例，保证可重复 Init / Close
    return nil
}
```

关键要求：

- `Enabled=false` 时，本文件所有方法都必须是 no-op。
- `Close()` 必须幂等，重复调用不报错。
- `Init()` 失败时不能留下半初始化状态。
- 日志只记录 endpoint 和 namespace，不记录用户名、密码、token。

### 5.2 `nacosx/config.go`

职责：拉取、监听、合并 Nacos 配置，并给 Viper 提供可复用的合并入口。

```go
type ConfigCenterConfig struct {
    Required     bool
    DataIDs      []string
    Group        string
    Timeout      time.Duration
}

func Fetch(dataID string) ([]byte, error) {
    // 1. 检查 client 是否已初始化
    // 2. 检查 dataID 是否为空
    // 3. 调用 Nacos config client 获取内容
    // 4. 返回原始 YAML 字节
}

func Listen(dataID string, onChange func(content string)) error {
    // 1. 注册监听
    // 2. 收到变更后回调
    // 3. 变更内容为空时按“配置被删除”处理
    // 4. 回调里不得抛 panic，异常必须收敛为错误日志
}
```

推荐启动顺序：

```text
读取本地 config/config.yaml
→ 读取 config/{env}.yaml
→ 读取 Nacos dataID
→ 环境变量覆盖
→ 最终 Unmarshal 到服务自己的 Config
```

推荐优先级：

```text
环境变量 > Nacos 配置 > 环境文件 > 基础文件
```

降级策略：

- `required=false`：Nacos 不可用时记录 warning，继续使用本地配置启动。
- `required=true`：Nacos 不可用时启动失败，适用于强管控环境。
- 配置拉取成功但解析失败：一律启动失败，不允许半配置启动。
- 敏感配置不入 Nacos，仍走 K8s Secret 或环境变量。

热更新只允许白名单字段：

```text
log.level
cors.allow_origins
http.timeout
grpc.call_timeout
限流阈值
功能开关
```

白名单之外的字段变更只记录 warning，不动态生效，避免连接池、租户库、JWT 密钥在运行中被误改。

### 5.3 `nacosx/registry.go`

职责：统一封装服务注册、反注册、IP 决策和引流等待。

```go
type RegistryConfig struct {
    ServiceName string
    Cluster     string
    Weight      float64
    Metadata    map[string]string
}

func Register(serviceName string, port int, meta map[string]string) error {
    // 1. 解析注册 IP：NACOS_REGISTER_IP > POD_IP > HOST_IP > 出网探测
    // 2. 构造 RegisterInstanceParam
    // 3. 调用 naming client 注册
    // 4. 记录 service/ip/port，供 Deregister 使用
}

func Deregister() error {
    // 1. 读取 Register 保存的 service/ip/port
    // 2. 调用 naming client 反注册
    // 3. 必须幂等，重复调用不报错
}

func DrainWait() time.Duration {
    // 返回 3~10 秒，给发现缓存和网关路由收敛留时间
}
```

IP 决策必须显式处理：

```text
NACOS_REGISTER_IP
POD_IP
HOST_IP
自动探测出网 IP
```

推荐：

- K8s：优先 `POD_IP`。
- Docker Compose：优先 `HOST_IP`。
- 裸机：优先显式 `NACOS_REGISTER_IP`。

注册时机：

- `server` 命令：启动完成后注册。
- `migrate` 命令：不注册。
- 健康检查失败：不注册。
- 服务启动失败：不注册，若已注册则立即反注册。

### 5.4 `nacosx/resolver.go`

职责：把 `nacos://devops-id` 转换为 gRPC 可用的地址列表，并订阅实例变化。

```go
func RegisterNacosResolver() {
    // 注册 scheme=nacos 的 resolver.Builder
}

type nacosResolver struct {
    cc resolver.ClientConn
}

func (r *nacosResolver) ResolveNow(resolver.ResolveNowOptions) {}

func (r *nacosResolver) Close() {}
```

解析逻辑：

```text
解析 nacos://devops-id
→ 调用 Nacos 查询服务名 devops-id
→ 过滤 healthy=true && enabled=true
→ 转换为 grpc resolver.Address
→ 调用 cc.UpdateState()
→ 订阅实例变化
→ 实例列表变化时再次 UpdateState()
```

配套改动：

- `grpc.Dial` 检测 `nacos://` 前缀。
- 自动注入 `round_robin` balancer。
- 客户端不感知 Nacos，只写 `idclient.endpoint: "nacos://devops-id"`。
- Nacos 查询失败时，使用 SDK 本地缓存，不主动断开已有 gRPC 连接。

### 5.5 启动与退出时序

启动：

```text
加载本地配置
→ 初始化日志
→ 初始化 Nacos client
→ 拉取并合并 Nacos 配置
→ 初始化 DB / Redis / gRPC / 业务组件
→ 启动 HTTP 或 gRPC Server
→ 注册到 Nacos
→ 标记 ready
```

退出：

```text
收到 SIGTERM
→ 标记 not-ready
→ Deregister
→ DrainWait
→ GracefulStop HTTP / gRPC
→ 关闭 idclient
→ 关闭 Redis
→ 关闭 DB
→ 关闭 Nacos client
```

两条硬规则：

- 先反注册，再停服务。
- 先排空，再关依赖。

### 5.6 实施检查点

1. `nacos.enabled=false` 时，系统行为必须与当前版本完全一致。
2. `nacos.config.required=false` 时，Nacos 不可用不能阻塞启动。
3. `nacos.enabled=false` 时，不产生任何 Nacos 注册行为。
4. `migrate` 命令必须跳过注册。
5. 所有新增代码只落在 `devops-infra`，业务层零侵入。
6. 文档中的代码块只表达设计意图，落地时需按实际 SDK API 调整。

---

## 6. 分阶段实施计划

### Phase 1 — 配置中心（建议先做，收益明确、风险最低）

| # | 输入 | 实施步骤 | 产出与验收 |
| --- | --- | --- | --- |
| 1.1 | 部署环境、MySQL、Nacos 镜像 | 部署 Nacos 2.x，创建 namespace `devops`、group `DEVOPS_GROUP`，开启鉴权 | 控制台可登录；鉴权生效；配置与服务命名空间隔离 |
| 1.2 | Nacos 连接信息、`ClientConfig`、`ConfigCenterConfig` | 实现 `nacosx/client.go` 和 `nacosx/config.go`，覆盖 `Init / Close / Fetch / Listen` | Nacos 不可用时按 `required` 决定是否启动失败；配置变更能触发回调 |
| 1.3 | 本地 YAML、环境变量、Nacos dataId | 改造 `config/viper.go` 为“基础文件 → 环境文件 → Nacos → 环境变量”合并链 | `config.Files()` 能输出 Nacos dataId；配置优先级可预测 |
| 1.4 | `devops-system` 配置、热更新白名单 | 迁移 `log` 与 `server` 非敏感配置，接入 L1 热更新 | 修改 Nacos 中的 `log.level` 后，无需重启进程即可生效 |
| 1.5 | `devops-id` 配置、热更新白名单 | 复用同一套 `nacosx`，迁移 `log` 与非敏感服务参数 | devops-id 与 devops-system 的配置来源、降级行为一致 |

**回退**：`nacos.enabled: false`。

### Phase 2 — 注册中心与服务发现

| # | 输入 | 实施步骤 | 产出与验收 |
| --- | --- | --- | --- |
| 2.1 | 服务名、注册 IP、端口、元数据 | 实现 `nacosx/registry.go`，devops-id 先注册 | Nacos 控制台可见健康实例；IP 可达；注册元数据完整 |
| 2.2 | `idclient.endpoint`、gRPC Dial 封装 | 实现 `nacosx/resolver.go`，让 `grpc.Dial` 支持 `nacos://devops-id` | system → id 的 gRPC 调用正常；实例列表变化能触发 rebalance |
| 2.3 | 服务启动配置、健康状态 | devops-system 注册，并暴露 readiness 状态 | 控制台可见两个服务；异常实例不会继续接收新流量 |
| 2.4 | 退出信号、DrainWait 配置 | 改造优雅退出：not-ready → Deregister → DrainWait → GracefulStop | 滚动发布期间持续压测，**0 次 502/连接拒绝** |
| 2.5 | 多副本 devops-id、压测工具 | 扩到 3 副本并验证负载均衡与发号唯一性 | ID 不重复；流量分摊；无异常日志 |

**验收要点（必做）**：滚动发布期间用 `hey`/`wrk` 持续打 `/api/v1/auth/login`，统计非 2xx。这是注册中心改造的**核心质量门**。

### Phase 3 — Higress 网关

| # | 输入 | 实施步骤 | 产出与验收 |
| --- | --- | --- | --- |
| 3.1 | Higress 版本、部署环境 | 部署 Higress；本地用 all-in-one，生产用 Helm | 控制台可访问；服务组件健康 |
| 3.2 | Nacos 地址、namespace、group | 配 `McpBridge`（type `nacos2`）与 Ingress，指向 `devops-system` | `curl http://<gateway>/healthz` 正常返回 |
| 3.3 | CORS 域名列表 | 网关统一处理 CORS，应用层关闭重复 CORS | 浏览器预检只返回一组 CORS 头 |
| 3.4 | 网关网段 | 更新应用 `trusted_proxies` | 登录日志记录真实客户端 IP |
| 3.5 | `/readyz`、健康检查配置 | 网关主动探活，摘除不健康实例 | 停掉一个 Pod 后，网关不再转发流量 |
| 3.6 | 限流阈值、日志格式 | 配置限流与访问日志插件 | 超限返回 429；访问日志字段完整 |

### Phase 4 — 增强（可选）

- `jwt-auth` Wasm 插件粗粒度拦截（决策 4 的 Phase 2 项）
- 更多 L1 热更项（`server.cors`、`idclient.*`）
- `jwt.signing_key` 双密钥轮转
- 多租户域名 → 租户路由（Higress 按 host 打 `X-Tenant-Code` 头，应用侧 Tenant 中间件读取）
- 基于 Nacos metadata 的灰度发布

---

## 7. 风险与回退

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| Nacos 集群不可用（配置中心） | 存量进程不受影响（已加载配置）；**新 Pod 起不来**（若 `required: true`） | `nacos.config.required: false`（默认），本地文件兜底；Nacos 3 节点部署 |
| Nacos 集群不可用（注册中心） | 已建立连接不受影响；新实例无法注册、resolver 拿到缓存实例列表 | SDK 本地缓存（`NotLoadCacheAtStart: false`）；`idclient.endpoint` 保留静态地址作为 fallback |
| 优雅退出时序错 | 发布期间 502 | 决策 9 + Phase 2.4 压测门禁 |
| 配置热更误伤 | 连接池重建、全员掉线 | 决策 3 的 L0/L1/L2 分级，一期只热更 `log.level` |
| Nacos 中明文密钥泄露 | 安全事故 | 决策 11：开鉴权 + 敏感项走 K8s Secret 环境变量注入 |
| 依赖膨胀 / 版本冲突 | 编译失败、二进制变大 | 只在 devops-infra 引入；`go work sync` 后 `go build ./... && go vet ./...` 全绿 |
| Higress all-in-one 非生产级 | 官方明确"未大规模生产验证" | 生产走 Helm 云原生模式，all-in-one 仅本地 |

**一键回退**：三个开关全部置 false，即回到今天的架构：

```yaml
nacos:
  enabled: false        # 总开关：registry + config 全部 no-op
```

配合 `idclient.endpoint` 回写静态地址，可完整回退。

---

## 8. 工作量预估

| 阶段 | 人日 | 说明 |
| --- | --- | --- |
| Phase 1 配置中心 | 2~3 | 含 Nacos 部署、nacosx 配置模块、viper 合并、单测 |
| Phase 2 注册中心 | 3~4 | 含 resolver、优雅退出改造、滚动发布压测（较耗时） |
| Phase 3 Higress | 2~3 | 含 helm 部署、McpBridge/Ingress 调试、CORS/代理头联调 |
| Phase 4 增强 | 3+ | 视选做范围 |
| **合计（P1~P3）** | **7~10 人日** | 不含联调等待与评审 |

---

## 9. 待确认问题

在动手前，以下四点需要确认（会显著影响实现细节）：

1. **部署形态**：生产跑在 K8s 还是 docker-compose/裸机混合？
   - 纯 K8s：Nacos 注册中心与 K8s Service 功能重叠，主要价值在于「给 Higress 提供统一服务来源」+「跨语言/跨集群」。
   - 混合部署：Nacos 的价值最大化（统一发现）。
2. **配置中心是否放敏感项**：决定是否需要引入「Secret → 环境变量 → 覆盖 Nacos 值」这条链路（会增加 viper 改造量）。
3. **是否需要 Higress 的 JWT 插件**：若需要，需接受密钥多一处同步点，并做 base64url 转换。
4. **`idgen.mode` 用哪个**：若固定 segment，可顺带下线 etcd，减少一个外部依赖。

---

## 附：本地验证环境（Phase 1 起可用）

```bash
# 目录：deploy/docker-compose.yml 新增
nacos:
  image: nacos/nacos-server:v2.4.3
  environment:
    MODE: standalone
    NACOS_AUTH_TOKEN: <openssl rand -base64 32>
    NACOS_AUTH_IDENTITY_KEY: serverIdentity
    NACOS_AUTH_IDENTITY_VALUE: security
    SPRING_DATASOURCE_PLATFORM: mysql
    MYSQL_SERVICE_HOST: mysql
    MYSQL_SERVICE_DB_NAME: nacos
    MYSQL_SERVICE_USER: nacos
    MYSQL_SERVICE_PASSWORD: nacospwd
  ports:
    - "8848:8848"   # HTTP 控制台 / OpenAPI
    - "9848:9848"   # gRPC（Nacos 2.x 客户端必开）
  depends_on:
    mysql: { condition: service_healthy }

higress:
  image: higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:latest
  volumes:
    - ./higress-data:/data
  ports:
    - "8001:8001"   # 控制台
    - "8080:8080"   # 网关 HTTP
    - "8443:8443"   # 网关 HTTPS
```

> Nacos standalone 用内置 Derby 亦可跑通，但重启丢配置；建议挂 MySQL（`nacos` 库需提前建好并导入 `nacos-mysql.sql`）。
