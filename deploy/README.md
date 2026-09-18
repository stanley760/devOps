# 部署说明

`deploy` 目录提供本地开发环境和生产环境的容器编排配置。开发环境使用独立的
`docker-compose-dev.yml`、`config/*-dev.yaml` 和启动脚本，不会覆盖生产编排使用的配置。

## 一键启动开发环境

开发编排文件包含以下服务：

```text
MySQL、Redis、Nacos、etcd、devops-id、devops-system、Higress
```

其中 `devops-id` 和 `devops-system` 使用本地 Dockerfile 构建，Higress 使用预构建镜像，
首次启动时由 Docker 自动拉取：

```text
higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/all-in-one:2.2.3
```

### Windows

在仓库根目录使用 PowerShell 执行：

```powershell
.\deploy\dev.ps1
```

### Linux/macOS

```bash
sh deploy/dev.sh
```

脚本会自动构建本地应用镜像、启动基础依赖、执行幂等数据库迁移，
然后启动 `devops-id`、`devops-system` 和 Higress。

### 常用命令

Windows：

```powershell
.\deploy\dev.ps1 -Command ps
.\deploy\dev.ps1 -Command logs
.\deploy\dev.ps1 -Command migrate
.\deploy\dev.ps1 -Command restart
.\deploy\dev.ps1 -Command down
```

### 本地启动 devops-system，Higress 仍作为统一入口

适合 IDE 调试 Go 代码：MySQL、Redis、Nacos、etcd、devops-id 和 Higress
运行在 Docker 中，`devops-system` 由宿主机进程启动。

先启动基础设施和网关：

```powershell
.\deploy\dev.ps1 -Command local-up
```

然后在 IDE 的 `devops-system` 启动配置中使用：

```text
APP_ENV=local
NACOS_REGISTER_IP=<宿主机局域网IPv4>
```

配置文件使用 [config/system-local.yaml](./config/system-local.yaml)。其中本地
进程通过 `127.0.0.1` 访问 Docker 已发布的 MySQL、Redis、Nacos；Nacos 注册的
`NACOS_REGISTER_IP` 必须是 Higress 容器可以访问的宿主机局域网地址，不能填写
`127.0.0.1`。服务仍监听 `:9999`，但只通过 Higress 访问：

```text
http://api.devops.local:8080/healthz
```

停止本地模式依赖：

```powershell
.\deploy\dev.ps1 -Command local-down
```

Linux/macOS 使用 `sh deploy/dev.sh local-up` 和 `sh deploy/dev.sh local-down`。

本地服务不需要加入 Docker Compose，也不需要发布 `9999:9999`。宿主机进程启动
后会向 Nacos 注册 `devops-system/<NACOS_REGISTER_IP>:9999`，Higress 的
`McpBridge` 从 Nacos 订阅 `DEVOPS_GROUP` 并将 `api.devops.local` 的请求转发
到该实例。Windows Docker Desktop 通常可访问宿主机局域网 IP；如果系统防火墙
拦截 Docker 网段到 9999 端口，需要放行该端口。

建议按以下顺序启动：

```powershell
# 1. 启动 MySQL、Redis、Nacos、etcd、devops-id 和 Higress
.\deploy\dev.ps1 -Command local-up

# 2. 另一个终端启动宿主机上的 devops-system
$env:APP_ENV = "local"
$env:NACOS_REGISTER_IP = "192.168.1.20"
go run .\devops-system server

# 3. 只通过 Higress 验证
curl.exe -H "Host: api.devops.local" http://localhost:8080/healthz
```

Linux/macOS：

```bash
sh deploy/dev.sh local-up
APP_ENV=local NACOS_REGISTER_IP=192.168.1.20 go run ./devops-system server
curl -H 'Host: api.devops.local' http://localhost:8080/healthz
```

如果本地有多个网卡，使用 Docker Desktop/虚拟机能够访问的那一个地址。可先
从宿主机验证服务：

```text
http://127.0.0.1:9999/healthz
```

但该地址只用于排障，客户端和前端代码必须始终使用 Higress 的
`http://api.devops.local:8080`。

Linux/macOS：

```bash
sh deploy/dev.sh ps
sh deploy/dev.sh logs
sh deploy/dev.sh migrate
sh deploy/dev.sh restart
sh deploy/dev.sh down
```

### 访问地址

| 服务 | 地址 |
| --- | --- |
| devops-system | 仅容器网络可达，不发布宿主机端口 |
| Higress 网关 | `http://localhost:8080/healthz`（Host: `api.devops.local`） |
| Higress 控制台 | `http://localhost:8001` |
| Nacos 控制台 | `http://localhost:8848/nacos` |

开发环境数据保存在带有 `dev-` 前缀的 Docker volume 中，与生产编排的数据卷隔离。
Higress 专属路由、限流、CORS 和 Kubernetes 说明见
[higress/README.md](./higress/README.md)。

## 生产/通用容器编排

生产/通用编排文件为 [docker-compose.yml](./docker-compose.yml)。Higress 在该文件中
通过 `gateway` profile 启动：

```bash
docker compose -f deploy/docker-compose.yml --profile gateway up -d
```
