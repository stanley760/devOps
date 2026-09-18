# Higress 适配说明

本目录既作为 Compose all-in-one 的 `/data` 挂载目录，也可作为 Kubernetes 资源清单参考。`mcpbridges`、`ingresses`、`wasmplugins` 是 Higress 文件配置后端识别的资源目录。

## Compose 启动

```bash
docker compose -f deploy/docker-compose.yml build
docker compose -f deploy/docker-compose.yml run --rm devops-id migrate
docker compose -f deploy/docker-compose.yml run --rm devops-system migrate
docker compose -f deploy/docker-compose.yml --profile gateway up -d
```

- Nacos 控制台：`http://localhost:8848/nacos`
- Higress 控制台：`http://localhost:8001`
- HTTP 网关：`http://localhost:8080`
- 路由域名：`api.devops.local`

验证路由：

```bash
curl -i -H 'Host: api.devops.local' http://localhost:8080/healthz
```

如需浏览器访问，可把 `127.0.0.1 api.devops.local` 加入本机 hosts。

开发环境的一键启动、服务依赖、访问地址和常用命令见
[deploy/README.md](../README.md)。

## 资源说明

- `mcpbridges/default.yaml`：使用 `nacos2` 协议对接 Nacos，并订阅 `DEVOPS_GROUP`。
- `ingresses/devops-system.yaml`：将 `api.devops.local` 路由到 Nacos 服务 `devops-system`。
- Higress 是后端服务的唯一外部入口；`devops-system:9999` 仅在容器/Kubernetes
  内部网络暴露，不应通过宿主机端口或公网负载均衡直接发布。
- `ingresses/devops-system-canary.yaml`：将约 10% 流量路由到 `devops-system-gray`。
- `wasmplugins/cluster-key-rate-limit.yaml`：通过 Redis 实现集群级每 IP 100 QPS 限流。
- `wasmplugins/cors.yaml`：在网关层统一处理跨域，应用侧不再包含 CORS 配置与实现，避免响应头重复。

Nacos 分组 `DEVOPS_GROUP` 在 Higress 目标地址中写作 `DEVOPS-GROUP`，这是 Higress 对下划线的转义规则。

## 灰度发布

灰度实例必须以 `devops-system-gray` 为服务名注册到同一个 Nacos 分组。当前示例按权重灰度：

```yaml
higress.io/canary-weight: "10"
```

也支持按 Header 或 Cookie 灰度，优先级为 Header > Cookie > 权重：

```yaml
higress.io/canary-by-header: "x-canary"
higress.io/canary-by-header-value: "true"
# 或
higress.io/canary-by-cookie: "canary"
```

Compose 中已提供 `devops-system-gray` 可选服务，使用 `--profile gray` 启动即可验证权重灰度。

## 限流配置

Compose 环境中 Redis 服务名为 `redis`、端口为 `6379`，默认密码与 `docker-compose.yml` 保持一致。迁移到 K8s 时请将 `service_name` 改为实际 Redis Service（如 `redis.devops.svc.cluster.local`），并同步修改密码和限流阈值。

## 跨域策略

CORS 已由 Higress 统一处理，应用侧不再实现或配置 CORS，避免应用侧与网关侧重复输出 CORS 响应头。新增来源域名时，应更新 `wasmplugins/cors.yaml` 并由 Higress 生效。

## JWT 认证

生产环境应在 Higress 的认证阶段为 `api.devops.local` 配置 `jwt-auth`：

1. 从 `Authorization: Bearer <token>` 读取 JWT；
2. 校验签名、`issuer=devops`、`exp` 和 `iat`；
3. 通过 `claims_to_headers` 将 `tid`、`tcode` 等 Claims 转发为内部 Header；
4. 使用 `override=true` 覆盖客户端同名 Header，避免伪造租户身份。

网关认证通过后仍需保留 `devops-system` 的 JWT 中间件。system 还负责
Redis 会话吊销检查、租户状态/过期检查以及租户上下文注入，不能仅凭
`X-Tenant-ID` 信任客户端身份。

当前 system 使用 HS256 时，Higress 需要配置同一对称 JWK；生产推荐改用
RS256，让 system 只持有私钥，Higress 只配置公钥 JWKS。JWT 密钥不得写入
本目录的公开配置文件。

## Kubernetes 部署

all-in-one 镜像适合本地和体验环境，生产环境应使用 Helm 部署 Higress：

```bash
helm repo add higress.io https://higress.cn/helm-charts
helm install higress -n higress-system higress.io/higress --create-namespace
kubectl apply -f deploy/higress/mcpbridges/default.yaml
kubectl apply -f deploy/higress/ingresses
kubectl apply -f deploy/higress/wasmplugins
```

Kubernetes 环境需将 `mcpbridges/default.yaml` 的 `domain` 改为实际 Nacos 地址，并确认 Higress Controller 可访问该地址的 8848 与 9848 端口。
