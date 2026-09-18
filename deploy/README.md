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
