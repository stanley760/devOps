# 部署说明

`deploy` 目录提供本地开发环境和生产环境的容器编排配置。

## 开发环境

开发环境**只启动中间件和可观测栈**，业务服务（`devops-system` / `devops-id`）由宿主机
`go run` 启动，便于 IDE 调试和热重载。

### 启动中间件

```powershell
.\deploy\dev.ps1
```

```bash
sh deploy/dev.sh
```

脚本会启动 MySQL、Redis、Nacos、etcd、Higress 网关和可观测栈（Jaeger/Prometheus/Grafana），
然后自动拉起本地业务服务：

- **PowerShell**：在两个独立的 cmd 窗口内分别 `go run` 启动 `devops-id` 与 `devops-system`，
  关闭对应窗口即停止该服务。
- **sh**：以后台进程启动，日志与 PID 记录在 `deploy/.dev-logs/` 下
  （`tail -f deploy/.dev-logs/devops-system.log` 查看输出）。

首次运行或 schema 变更后，先执行数据库迁移：

```powershell
.\deploy\dev.ps1 -Command migrate
```

```bash
sh deploy/dev.sh migrate
```

> **NACOS_REGISTER_IP**：本地进程注册到 Nacos 的 IP 必须是宿主机局域网 IPv4
> （不能是 `127.0.0.1`），否则 Higress 容器无法回连业务服务。
> ```powershell
> $env:NACOS_REGISTER_IP = "192.168.1.20"
> ```

### 常用命令

```powershell
.\deploy\dev.ps1 -Command ps       # 查看容器状态
.\deploy\dev.ps1 -Command logs     # 跟踪日志
.\deploy\dev.ps1 -Command migrate  # 数据库迁移
.\deploy\dev.ps1 -Command restart  # 重启中间件
.\deploy\dev.ps1 -Command app      # 仅启动本地业务服务（不动容器）
.\deploy\dev.ps1 -Command sync     # 同步 deploy/higress 配置到命名卷并重启网关
.\deploy\dev.ps1 -Command down     # 停止并清理容器
```

```bash
sh deploy/dev.sh ps       # 查看容器状态
sh deploy/dev.sh logs     # 跟踪日志
sh deploy/dev.sh migrate  # 数据库迁移
sh deploy/dev.sh restart  # 重启中间件
sh deploy/dev.sh app      # 仅启动本地业务服务（不动容器）
sh deploy/dev.sh stop    # 停止本地业务服务（不动容器）
sh deploy/dev.sh sync    # 同步 deploy/higress 配置到命名卷并重启网关
sh deploy/dev.sh down     # 停止并清理容器
```

### Higress 配置与命名卷

Higress 的 `/data` 使用命名卷 `higress-data`（避免 Windows bind mount 跨文件系统导致的
高 CPU）。`deploy/higress/` 仍是配置的唯一真源：compose 的 `higress-init` 一次性服务
在网关启动前自动把其中的 `mcpbridges/`、`ingresses/`、`wasmplugins/` 灌入卷，
每次 `up` 都会刷新；修改 yaml 后执行 `sync` 可立即生效（重跑灌入 + 重启网关）。
init 会覆盖这三个目录，控制台里做的路由/插件改动请先移回 yaml。

### 访问地址

| 服务 | 地址 |
| --- | --- |
| devops-system | `http://localhost:9999/healthz`（本地进程） |
| Higress 网关 | `http://localhost:8080/healthz`（Host: `api.devops.local`） |
| Higress 控制台 | `http://localhost:8001` |
| Nacos 控制台 | `http://localhost:8848/nacos` |
| Jaeger UI | `http://localhost:16686` |
| Prometheus | `http://localhost:9090` |
| Alertmanager | `http://localhost:9093`（dev 告警仅 UI 可见，生产接 webhook） |
| Grafana | `http://localhost:3000`（admin/admin） |

告警：规则在 [prometheus/alerts.yml](./prometheus/alerts.yml)（目标失联 / HTTP 5xx /
P99 / gRPC 错误率 / SQL·Redis 连接池等待等），命中后经 Alertmanager 路由；dev 的接收器
为空（仅 UI 可见），生产替换 `alertmanager/alertmanager.yml` 中的 dev-sink 为钉钉/邮件 webhook。

开发环境数据保存在带有 `dev-` 前缀的 Docker volume 中，与生产编排的数据卷隔离。
Higress 专属路由、限流、CORS 和 Kubernetes 说明见
[higress/README.md](./higress/README.md)。

### 配置文件对应关系

| 启动方式 | 加载的配置 | 说明 |
| --- | --- | --- |
| 本地 `go run` | `devops-system/config/devops-system-dev.yaml` | 地址 `127.0.0.1`，连宿主机端口 |
| 本地 `go run` | `devops-id/config/devops-id-dev.yaml` | etcd `127.0.0.1:2379` |
| `migrate` 容器 | `deploy/config/system-dev.yaml` | 地址用容器服务名（mysql/redis/nacos） |

## 生产/通用容器编排

生产/通用编排文件为 [docker-compose.yml](./docker-compose.yml)。Higress 在该文件中
通过 `gateway` profile 启动：

```bash
docker compose -f deploy/docker-compose.yml --profile gateway up -d
```
