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
然后提示如何在本地启动业务服务。

### 启动业务服务

中间件就绪后，在宿主机打开三个终端：

```powershell
# 终端 1：启动 devops-id（gRPC 发号 :9090）
cd devops-id && go run . server

# 终端 2：执行数据库迁移（首次或 schema 变更后）
.\deploy\dev.ps1 -Command migrate

# 终端 3：启动 devops-system（HTTP :9999）
cd devops-system && go run . server
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
.\deploy\dev.ps1 -Command down     # 停止并清理
```

### 访问地址

| 服务 | 地址 |
| --- | --- |
| devops-system | `http://localhost:9999/healthz`（本地进程） |
| Higress 网关 | `http://localhost:8080/healthz`（Host: `api.devops.local`） |
| Higress 控制台 | `http://localhost:8001` |
| Nacos 控制台 | `http://localhost:8848/nacos` |
| Jaeger UI | `http://localhost:16686` |
| Prometheus | `http://localhost:9090` |
| Grafana | `http://localhost:3000`（admin/admin） |

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
