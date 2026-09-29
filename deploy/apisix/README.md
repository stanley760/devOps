# APISIX 网关配置与部署

本目录是网关的**唯一配置真源**：`config.yaml`（实例配置）+ `apisix.yaml`（路由/上游，声明式）。
两者都是 git 管理的普通文件，改完即生效（热加载），没有任何"灌入/同步"步骤。

## 一、架构与部署形态

| 形态 | 方式 | 适用 | 说明 |
|---|---|---|---|
| dev / docker 生产 | APISIX standalone 数据面 + YAML 配置源 | 单机 | 本目录文件直接挂载进容器，即本文档主角 |
| K8s | APISIX Ingress Controller（Helm） | 正式生产 | 见「九」，用 ApisixRoute CRD，不走本目录 |

三种形态下业务流量路径一致：

```
Client（前端/脚本，Host: api.devops.local）
   ──▶ APISIX :9080/:9443（容器外发布为 8080/8443）
         │  路由匹配（host + uri）→ 插件链（cors/prometheus/traffic-split）
         ▼
       Nacos 服务发现解析 devops-system / devops-system-gray 实例
         ──▶ devops-system :9999（HTTP）
```

选用 APISIX 的原因（相对原 higress all-in-one）：
- **声明式配置**：standalone 模式下 git 文件即运行时配置，无控制台/etcd/一次性灌入容器；
- **单个轻量容器**：nginx+lua 数据面（~512m 内存），无 pilot/console/内置 o11y 负担；
- Nacos 服务发现、按权重/Header/Cookie 灰度（traffic-split）、CORS 全部覆盖原配置职能。

## 二、Dev 环境（standalone + 声明式文件）

### 2.1 组成

`docker-compose-dev.yml` 的 `apisix` 服务：

```yaml
apisix:
  image: apache/apisix:3.18.0-debian
  ports:
    - "8080:9080"     # HTTP 网关（对外端口与 higress 时代保持一致，调用方不变）
    - "8443:9443"     # HTTPS 网关（TLS 需配 ssls 资源，见 7.4）
  volumes:
    - ./apisix/config.yaml:/usr/local/apisix/conf/config.yaml:ro
    - ./apisix/apisix.yaml:/usr/local/apisix/conf/apisix.yaml:ro
    - apisix-logs:/usr/local/apisix/logs      # 运行时日志用命名卷（勿 bind mount，见 2.3）
  depends_on:
    nacos: {condition: service_healthy}
```

- 两个配置文件是**只读 bind mount 的小文件**——安全；运行时持续写状态的只有 logs 目录，
  用命名卷承接（Windows NTFS→WSL2 bind mount 高 CPU 的教训，与 higress 时代相同）。
- 没有 admin API、没有控制台端口（8001 随 higress 一起消失）；配置变更一律改本目录 git 文件。

### 2.2 配置变更与热加载

- **路由/插件**（`apisix.yaml`）：APISIX 每秒检查文件 mtime，变更约 1 秒内热加载，
  **不需要重启**。Windows bind mount 偶发 mtime 不触发时，兜底：
  `docker compose -f deploy/docker-compose-dev.yml restart apisix`。
- **实例配置**（`config.yaml`）：不含热加载逻辑，改后需 restart apisix。
- `apisix.yaml` **末尾的 `#END` 标记必需**，缺失会导致解析失败（容器反复重启）。

### 2.3 已知坑（排查手册）

| 症状 | 原因 | 解法 |
|---|---|---|
| 容器反复重启 | `apisix.yaml` 缺 `#END`，或 yaml 语法错 | `docker compose logs apisix` 看解析报错；补标记 |
| 502/503 Bad Gateway / no healthy upstream | Nacos 里没有健康实例（业务服务没起 / NACOS_REGISTER_IP 不可达）；无健康节点 APISIX 回 503，上游连接失败回 502 | 先 `curl http://localhost:8848/nacos` 确认 Nacos；确认服务已注册 |
| 404 Not Found | Host 头不匹配路由（`api.devops.local`）或 uri 不在 `/api`、`/.well-known/jwks.json` 下（运维端点 `/healthz` 等不经网关，见四） | `curl -i -H 'Host: api.devops.local' http://localhost:8080/api/...` |
| 改了 `apisix.yaml` 不生效 | Windows bind mount mtime 未传播（热加载没触发） | `docker compose -f deploy/docker-compose-dev.yml restart apisix` |
| 实例下线后仍被转发 | Nacos v1 HTTP 轮询的感知窗口（`fetch_interval: 10` 秒） | 等 10s；紧急时可 restart apisix |
| 8443 握手失败 | 没配 ssls 证书资源 | 见 7.4 |

### 2.4 镜像拉取（国内网络）

`apache/apisix` 不在 daocloud 等公共 mirror 白名单内，直接 `docker pull apache/apisix` 会
报 `denied`。已验证可行的拉法（显式 registry 地址绕过 mirror 列表，走 Docker Desktop 系统代理）：

```bash
docker pull registry-1.docker.io/apache/apisix:3.18.0-debian
docker tag registry-1.docker.io/apache/apisix:3.18.0-debian apache/apisix:3.18.0-debian
```

3.14+ 的镜像 tag 带发行版后缀（`-debian` / `-ubuntu` / `-redhat`），无裸版本号 tag。

## 三、服务发现：Nacos

`config.yaml` 的 `discovery.nacos` 指向 compose 内的 `nacos:8848`；每个 upstream 用
`discovery_args` 指定命名空间/分组（与业务注册侧一致：namespace `public`、group `DEVOPS_GROUP`）：

```yaml
- id: devops-system
  type: roundrobin
  discovery_type: nacos
  service_name: devops-system
  discovery_args:
    namespace_id: public
    group_name: DEVOPS_GROUP
```

与 higress 时代的差异：
- higress 用 **nacos2（gRPC 订阅）**，实例上下线秒级感知；APISIX 走 **v1 HTTP 轮询**，
  感知窗口 = `fetch_interval`（dev 设 10s）。灰度实例停止后最多 10 秒内仍可能被转发。
- 业务侧注册参数**完全不变**：服务名、分组、`NACOS_REGISTER_IP`（必须是网关容器可回连的
  局域网 IP，见各模块 `.env.example`）。

## 四、路由与上游

`apisix.yaml` 定义 2 个 upstream（主/灰度）+ 2 条 route：

| route | host | uri | 上游 | 插件 |
|---|---|---|---|---|
| devops-system | api.devops.local | `/api`、`/.well-known/jwks.json`（含子路径） | 主 upstream + traffic-split 10% 灰度 | cors、prometheus、traffic-split |
| devops-system-gray-route | gray.devops.local | 同上 | 100% 灰度 upstream | cors、prometheus |

- uri 写法：精确 + 通配各一条（`/api` 与 `/api/*`），对应原 higress 的 Prefix 语义。
- 路径**原样透传**（无 rewrite），与 higress 一致。
- 仅业务 API 与 JWKS 经网关；运维端点（`/healthz` `/readyz` `/metrics` `/apidoc`）
  按原 higress 口径仅内网直连，禁止经公网网关暴露。
- JWKS 端点（`/.well-known/jwks.json`）经网关暴露，供需要公钥验签的调用方拉取；
  业务自身的 JWT 校验仍在进程内完成（见 7.3）。

## 五、灰度发布（traffic-split）

`devops-system-gray` 以独立服务名注册到**同一** Nacos 分组，靠服务名区分。切流方式：

**权重灰度（当前配置）**——主路由上 10% 流量进灰度：

```yaml
traffic-split:
  rules:
    - weighted_upstreams:
        - upstream_id: devops-system-gray
          weight: 10
        - weight: 90      # 空条目 = 路由自身 upstream（主实例）
```

**Header/Cookie 定向灰度（需要时按注释启用）**——匹配条件为 lua-resty-expr：

```yaml
rules:
  - match:
      - vars:
          - ["http_x_canary", "==", "true"]     # 请求头 x-canary: true
    weighted_upstreams:
      - upstream_id: devops-system-gray
```

优先级：带 match 的规则先于兜底权重规则。操作流程：灰度实例注册 → 改本文件 →
热加载生效 → Grafana「网关边缘」面板观察 → 全量后以主服务名发布并删除灰度路由/规则。

## 六、插件

| 原 higress wasm 插件 | APISIX 对应 | 状态 |
|---|---|---|
| cors-1.0.0 | 路由级 `cors` 插件 | 参数原样迁移（见 apisix.yaml） |
| geo-ip-1.0.0 | 无（APISIX 无 geoip 插件） | **移除**：Go 服务不消费任何 Geo header，`c.ClientIP()` 走 trusted_proxies，功能无人使用 |

- **限流（原设计未落地，APISIX 落地路径）**：`limit-count`/`limit-req` 单机限流可直接挂路由；
  集群限流配 `policy: redis`（`redis_host: redis.devops.local`，compose 已有该网络别名）。
- **客户端 IP**：APISIX 默认可信所有代理，如需精确 `X-Real-IP` 语义可在路由上挂
  `real-ip` 插件。

### jwt-auth / 网关层验签（不做，与 higress 时代相同）

进程内已有完整 JWT 校验链（`devops-infra/security`），且本项目 JWT 为 RS256/JWKS 语义；
APISIX 的 `jwt-auth` 插件只支持 HS256 共享密钥，**不在网关重复验签**。如未来需要网关层
验签，用 `openid-connect`（对接 IdP）或自写 ext-plugin。

## 七、可观测性

- **指标**：路由上挂 `prometheus` 插件 + `config.yaml` 的 `plugin_attr.prometheus`
  （`export_addr: 0.0.0.0:9091`，容器内必须非 127.0.0.1）。Prometheus 抓取：
  `apisix:9091/apisix/prometheus/metrics`（job `apisix-gateway`，见
  `deploy/prometheus/prometheus.yml`）。
- **Grafana**：`devops-red.json` 的「网关边缘」面板已改用 `apisix_http_status` /
  `apisix_http_latency_bucket`。注意 APISIX 指标无 upstream 节点维度，同一路由内
  主/灰度实例不拆分序列，灰度占比以 traffic-split 权重为准。
- **日志**：访问/错误日志在 `apisix-logs` 命名卷（`/usr/local/apisix/logs`）：
  `docker compose -f deploy/docker-compose-dev.yml exec apisix tail -f logs/error.log`。
- **告警**：`alerts.yml` 面向业务服务指标（`devops-services` job），网关层告警可后续加
  `up{job="apisix-gateway"} == 0` 与边缘 5xx 比例规则。

## 八、Docker 生产部署

单机生产沿用 standalone 模式（`docker-compose.yml` 的 `apisix` 服务，`profiles: ["gateway"]`）：
git 文件即配置，升级即 `docker pull` + `restart`，运维面最小。

多节点/需要 admin API 动态配置时再切 traditional 模式（+ etcd）：

- 官方编排参考 [apache/apisix-docker](https://github.com/apache/apisix-docker) `compose/docker-compose-release.yaml`
  （etcd + apisix，config.yaml 里 `deployment.role: traditional` + `config_provider: etcd`）；
- 切换后 `apisix.yaml` 声明式文件作废，路由经 admin API（或 [apisix-ingress-controller](https://github.com/apache/apisix-ingress-controller)）下发；
  本目录退化为初始配置的迁移源。

生产化要点：
- 网关端口只经 TLS（443）对外，HTTP 80 做 301；console 类端口不存在（standalone 无 admin API）。
- Nacos 生产开启鉴权后，`config.yaml` 的 `discovery.nacos.host` 改为
  `http://user:pass@nacos:8848`（APISIX 会按原值编码）。
- 镜像 tag 固定（`3.18.0-debian`），升级前查 [apisix-docker releases](https://github.com/apache/apisix-docker/releases)。
- Redis/MySQL 等密码经 `.env`（不提交）注入，compose 中 `${VAR}`。

## 九、Kubernetes 部署（正式生产推荐）

K8s 下不用 Nacos 发现（`deploy/k8s/devops.yaml` 中 `nacos.enabled: false`，服务间走
K8s Service DNS），网关用 APISIX Ingress Controller + CRD：

```bash
helm repo add apisix https://charts.apiseven.com
helm install apisix -n ingress-apisix apisix/apisix \
  --create-namespace --set ingress-controller.enabled=true
```

路由声明（`ApisixRoute` CRD，对应本目录 apisix.yaml 的主路由）：

```yaml
apiVersion: apisix.apache.org/v2
kind: ApisixRoute
metadata:
  name: devops-system
  namespace: devops
spec:
  http:
    - name: devops-system
      match:
        hosts: [api.devops.example.com]
        paths: ["/api/*"]
      backends:
        - serviceName: devops-system
          servicePort: 9999
          weight: 90
        - serviceName: devops-system-gray   # K8s 下灰度用独立 Service/label
          servicePort: 9999
          weight: 10
      plugins:
        - name: cors
          enable: true
          config:
            # cors 插件 schema：allow_origins 为逗号分隔字符串，不接受数组（同 apisix.yaml）
            allow_origins: https://console.example.com
            allow_credentials: true
```

## 附：常用命令速查

```bash
# dev：启动（容器 + 拉起本地业务服务）
sh deploy/dev.sh            # 或 .\deploy\dev.ps1
sh deploy/dev.sh app        # 只启动本地业务服务

# 路由验证（与 higress 时代完全一致；/healthz 等运维端点不经网关，改用 JWKS 探活）
curl -i -H 'Host: api.devops.local' http://localhost:8080/api/v1/auth/login
curl -i -H 'Host: gray.devops.local' http://localhost:8080/.well-known/jwks.json    # 灰度直达

# 改配置后（apisix.yaml 热加载未触发时兜底）
docker compose -f deploy/docker-compose-dev.yml restart apisix

# 容器内排查
docker compose -f deploy/docker-compose-dev.yml exec apisix tail -f logs/error.log
docker compose -f deploy/docker-compose-dev.yml exec apisix cat /usr/local/apisix/conf/apisix.yaml

# 指标自检（应返回 apisix_* 指标文本）
docker compose -f deploy/docker-compose-dev.yml exec apisix curl -s http://127.0.0.1:9091/apisix/prometheus/metrics | head
```
