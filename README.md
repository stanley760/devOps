# devOps — 用户权限系统（多租户）

![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)
![Gin](https://img.shields.io/badge/Gin-v1.12-00ADD8?logo=gin)
![GORM](https://img.shields.io/badge/GORM-v1.31-00ADD8)
![Casbin](https://img.shields.io/badge/Casbin-v3-00ADD8)

Go 工作区（go.work）三模块：**devops-infra**（共享基础设施）+ **devops-system**（用户权限系统）+ **devops-id**（分布式 ID 发号服务，美团 Leaf）。

数据模型对齐 [RuoYi-vue-pro（yudao）](https://github.com/YunaiV/ruoyi-vue-pro) 精简版。

## 特性 / Features

- **JWT 双 token**：access（短效）+ refresh（Redis 会话，支持登出/踢下线/租户停用全员下线）
- **RBAC + 域模型**：Casbin `g, r, p` 域模型（`g` 域内用户-角色、`g2` 平台管理员），菜单权限按钮级控制
- **单库多租户**：所有租户共享数据库，通过 `tenant_id` 判别列隔离；GORM 插件自动注入过滤（fail-closed，无租户上下文直接报错防串数据）
- **分布式 ID（Snowflake）**：etcd 按 `leaf_name + biz_tag` 分配独立 worker ID（lease 保活、宕机 TTL 释放），同 `biz_tag` 内唯一且趋势递增；时钟回拨保护（<5ms 自旋、≥5ms 拒发）
- **工程化**：统一响应信封、结构化异步日志（大小/时间双滚动）、CORS、优雅退出链、多环境配置（`APP_ENV` 覆盖）

## 目录 / TOC

- [快速开始](#快速开始--quick-start)
- [CLI](#cli)
- [配置](#配置--configuration)
- [API](#api)
- [架构要点](#架构要点)
- [测试](#测试--testing)
- [Roadmap](#roadmap)

## 快速开始 / Quick Start

### 前置依赖

| 组件     | 用途                                                                 | 必需 |
| -------- | -------------------------------------------------------------------- | ---- |
| Go 1.26+ | 编译                                                                 | 是   |
| MySQL 8  | 业务库（`t_tenant` + `t_system_*` + `casbin_rule`）                 | 是   |
| Redis    | refresh 会话                                                         | 是   |
| etcd     | Snowflake worker 槽位协调                                            | 是   |

建库（一次）：

```sql
CREATE DATABASE devops DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'devops'@'%' IDENTIFIED BY 'changeme';
GRANT ALL PRIVILEGES ON devops.* TO 'devops'@'%';
```

### 启动

```bash
cd devops-id && go run . server    # gRPC :9990（Snowflake 发号）

cd devops-system && go run . migrate  # 建业务表 + 种子（租户 default、admin/Admin@123、菜单树、角色）
cd devops-system && go run . server   # HTTP :8080
```

### 验证

```bash
# 登录（admin / Admin@123，默认租户 default）
curl -s -X POST "http://127.0.0.1:8080/system/api/v1/auth/login" \
  -H "Host: api.devops.local" \
  -H "Content-Type: application/json" \
  -d '{"tenant_code":"default","username":"admin","password":"Admin@123"}'
# → {"code":200,"msg":"ok","data":{"access_token":"...","refresh_token":"...","expires_in":7200,"tenant":{"id":"1","name":"devOps","code":"default"}}}

# 携带 token 查询用户列表
curl -s http://localhost:8080/system/api/v1/users?page_no=1&page_size=10 \
  -H 'Authorization: Bearer <access_token>'
```

## CLI

```text
devops-system            用户权限系统
  server                 启动 HTTP 服务（:8080）
  migrate                建表 + 复合唯一索引 + 种子数据（幂等）

devops-id                分布式 ID 发号服务
  server                 启动 gRPC Snowflake 发号服务（:9990）
```

全局参数：`-f, --config-file` 指定配置路径（默认 `config/config.yaml`）；`--version` 查看版本。

## protobuf / gRPC 契约

devops-id 的 protobuf 源文件和生成文件位于 `devops-id` 模块：

| 文件                                                                       | 类型                      | 作用                                                                                       |
| -------------------------------------------------------------------------- | ------------------------- | ------------------------------------------------------------------------------------------ |
| [`proto/idgen.proto`](./devops-id/proto/idgen.proto)                       | protobuf 源文件           | 定义 `idgen.IdGen` 服务、请求/响应消息及字段语义；修改 gRPC 契约时只编辑此文件             |
| [`pkg/idgenpb/idgen.pb.go`](./devops-id/pkg/idgenpb/idgen.pb.go)           | `protoc-gen-go` 生成      | 生成 `NextRequest`、`NextReply`、`BatchNextRequest`、`BatchNextReply` 等 protobuf 消息类型 |
| [`pkg/idgenpb/idgen_grpc.pb.go`](./devops-id/pkg/idgenpb/idgen_grpc.pb.go) | `protoc-gen-go-grpc` 生成 | 生成 `IdGenClient`、`IdGenServer`、RPC handler 和 `RegisterIdGenServer`                    |

生成的 `.pb.go` 文件禁止手工修改；服务端实现位于
[`internal/interfaces/grpc/idgen.go`](./devops-id/internal/interfaces/grpc/idgen.go)，启动时注册生成的 gRPC server。
其中 `Next` 用于单个取号，`BatchNext` 原子分配一批 ID，是
[`pkg/idclient/idclient.go`](./devops-id/pkg/idclient/idclient.go) 客户端预取缓冲实际使用的 RPC。

生成产物**提交入库**（Go 惯例：`go build` 没有"先生成再编译"的阶段，`go get`/`go install`
也不会执行钩子，不提交则新 clone 无法编译）。只有修改 `proto/idgen.proto` 后才需要重新生成：

```bash
# 一次性安装工具链（protoc 需自行安装，如 brew install protobuf）
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

cd devops-id
protoc --proto_path=proto \
  --go_out=pkg/idgenpb --go_opt=paths=source_relative \
  --go-grpc_out=pkg/idgenpb --go-grpc_opt=paths=source_relative \
  proto/idgen.proto
```

生成文件头部记录了工具版本（如 `protoc-gen-go v1.36.12`），与协作者环境不一致时以仓库内产物为准。

请求约束和错误语义：

- `biz_tag` 不能为空，且必须是安全的路径组件；无需数据库预注册。
- `BatchNext.count` 必须在 `1..100000`；客户端的 `batch` 只应配置为业务需要的较小预取批量。
- 同一 `biz_tag` 内 ID 唯一、趋势递增，批内单调递增；服务重启或多副本缓冲时不保证严格连续。
- 不同 `biz_tag` 的 ID 空间独立，允许出现相同 ID。
- `InvalidArgument` 表示参数非法，`Unavailable` 表示 etcd 不可用、时钟回拨或 lease 失效；只有 `Unavailable` 建议重试。

## 配置 / Configuration

「基础 + 覆盖」模式：`config/config.yaml` 全量公共项，`config/{dev,test,prod}.yaml` 仅覆盖差异字段，`APP_ENV` 选择环境。

devops-system 关键段：

| 段         | 关键项                                                               | 说明                                                                                                       |
| ---------- | -------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `server`   | `port` / `trusted_proxies` / `cors.allow_origins`                    | HTTP 端口、可信代理、跨域白名单（空则不启用 CORS）                                                         |
| `mysql`    | `host` / `database` …                                                | 业务库连接                                                                                                 |
| `redis`    | `host` / `db`                                                        | 会话存储                                                                                                   |
| `jwt`      | `signing_key`（≥32 位）/ `access_expire` / `refresh_expire`          | 双 token 参数                                                                                              |
| `tenant`   | `default_code`                                                       | 登录未指定租户时使用的默认租户编码                                                                         |
| `idclient` | `endpoint` / `batch` / `low_water` / `dial_timeout` / `call_timeout` | devops-id gRPC 接入；`batch=1` 关闭预取，`low_water` 触发异步补充，两个 timeout 分别限制拨号和单次取号调用 |

devops-id 关键段：

| 段      | 关键项                                               | 说明                   |
| ------- | ---------------------------------------------------- | ---------------------- |
| `idgen` | `leaf_name` / `worker_cache`                        | worker 命名空间与本地缓存目录 |
| `etcd`  | `endpoints` / `lease_ttl`                           | worker 槽位协调与释放延迟 |

devops-id 配置示例：

```yaml
idgen:
  leaf_name: devops-id
  worker_cache: data
etcd:
  endpoints: ["192.168.215.3:2379"]
  lease_ttl: 10s
```

观察 worker 槽位：

```bash
etcdctl --endpoints=192.168.215.3:2379 get --prefix idgen/snowflake/devops-id/
```

每个 `biz_tag` 都会使用 `idgen/snowflake/{leaf_name}/{biz_tag}/` 独立槽位前缀。服务启动时不分配 worker，首次收到某个 `biz_tag` 的请求后才懒加载对应生成器。

jwt.signing_key 生成规则

jwt.signing_key 应使用密码学安全的随机字符串，至少 32 字节（256 位）。不要使用固定示例值或普通随机数。
Windows PowerShell 生成：

```powershell
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$rng.Dispose()
[Convert]::ToBase64String($bytes)
```
将结果写入 system-dev.yaml：
```yaml
jwt:
  signing_key: "mM9z6mYxJ5vQ3K8rT2pL7wN4sB1cD6eF0gH2jK5lP8Q="
```

使用 OpenSSL

如果已安装 OpenSSL：
```bash
openssl rand -base64 32
```
或者生成十六进制字符串：
```bash
openssl rand -hex 32
```
使用 Linux/macOS
```bash
openssl rand -base64 32
```
也可以：
```bash
python -c "import secrets; print(secrets.token_urlsafe(32))"
```
注意事项
开发环境和生产环境应使用不同的 signing_key。
不要将真实生产密钥提交到 Git。
修改 signing_key 后，之前签发的 JWT 会全部失效，需要重新登录。
生成后建议检查长度：
```bash
$key = "你的密钥"
$key.Length
```

Base64 形式的 32 字节随机值通常是 44 个字符，长度满足当前 HS256 配置要求。
## API

统一前缀 `/api/v1`，统一信封 `{"code", "msg", "data"}`（code 与 HTTP 状态一致）；`types.ID` 序列化为字符串（防 JS 精度丢失）。

| 分组 | 路由                                                                                                      | 鉴权                                                           |
| ---- | --------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| 认证 | `POST /auth/login` `POST /auth/refresh`                                                                   | 公开（login 可带 `tenant_code`，缺省取 `tenant.default_code`） |
| 认证 | `POST /auth/logout` `GET /auth/info` `GET /auth/routers` `PUT /auth/profile` `PUT /auth/profile/password` | JWT                                                            |
| 用户 | `GET/POST /users` `GET/PUT/DELETE /users/:id` `PUT /users/:id/password`                                   | JWT + 租户 + `system:user:*`                                   |
| 角色 | `GET/POST /roles` `GET/PUT/DELETE /roles/:id` `PUT /roles/:id/menus`                                      | JWT + 租户 + `system:role:*`                                   |
| 菜单 | `GET/POST /menus` `GET/PUT/DELETE /menus/:id`                                                             | JWT + 租户 + `system:menu:*`                                   |
| 部门 | `GET/POST /depts` `GET/PUT/DELETE /depts/:id`                                                             | JWT + 租户 + `system:dept:*`                                   |
| 岗位 | `GET/POST /posts` `GET/PUT/DELETE /posts/:id`                                                             | JWT + 租户 + `system:post:*`                                   |
| 租户 | `GET/POST /tenants` `GET/PUT/DELETE /tenants/:id` `PUT /tenants/:id/status`                               | JWT + **平台**（`g2` 平台管理员 + `platform:tenant:*`）        |

中间件链：`Recovery → AccessLog → CORS →（公开路由 | JWT → Tenant → Perm → 业务 | JWT → PlatformPerm → 租户管理）`。

## 架构要点

- **分层与装配**：`cmd` 管理启动顺序和资源生命周期，`internal/app` 用 Wire 编译期装配业务对象图，`router` 显式组合业务模块路由；请求链为 `router → api → service → dao → model`。`devops-infra` 只提供通用基础设施，不依赖业务模块
- **新增业务模块**：提供模块自己的 Service/Handler/路由注册器，在 `router.Modules` 与 `internal/app/providers.go` 中显式加入；重新生成并提交 `internal/app/wire_gen.go`。不向公共 `Register` 追加业务 Service 参数
- **Wire 生成**：在 `devops-system` 目录执行 `go generate -tags=wireinject ./internal/app`。生成文件提交入库，常规 build/test 不需要安装或运行 Wire 生成器
- **租户隔离 fail-closed**：纯模型查询由 GORM `TenantPlugin` 自动注入 `tenant_id`；原生 JOIN 由 dao 显式过滤（插件注入裸列会歧义）
- **软删除改名**：删除用户/角色等唯一字段实体时改名（`admin` → `admin_del_1694...`），允许同名重建
- **ID 空间隔离**：Snowflake worker 按 `leaf_name + biz_tag` 独立分配；同 `biz_tag` 唯一且趋势递增，不同 `biz_tag` 允许重号。DTO 层 ID 字符串化，避免 JavaScript 精度丢失
- **事务边界**：DB 事务只管业务表（user_role/role_menu），casbin 策略在事务外同步更新（gorm-adapter 自持事务）；失败补偿 + 重试由调用方语义保证
- **优雅退出**：HTTP 停止并排空在途请求后，按初始化逆序注销 Nacos、关闭 idclient/Redis/authz/MySQL；启动中途失败也执行已完成资源的清理，并汇总清理错误。snowflake 退出 revoke 所有已分配 tag 的 lease 并释放槽位

## 测试 / Testing

```bash
go work sync
go test ./devops-infra/... ./devops-id/... ./devops-system/...

# 验证 Wire injector 与已提交的生成代码保持一致
cd devops-system
go generate -tags=wireinject ./internal/app
git diff --exit-code -- internal/app/wire_gen.go

cd ../devops-id
go generate -tags=wireinject ./internal/app
git diff --exit-code -- internal/app/wire_gen.go
```

覆盖：infra（日志滚动、panic 恢复、CORS、响应信封）、id 集成测试（需本地 `:9990` 服务在跑，未启动自动 Skip）。

## Roadmap

- [ ] 操作/登录日志、账号配额、行级数据权限
- [ ] 可视化AI agent 数据
- [ ] 用户员工提交数据管理
- [ ] AI agent 数据过程分析
- [ ] AI agent 和用户行为数据分析
## Docker 开发环境故障排查

单节点开发环境使用独立编排文件：

```powershell
.\deploy\dev.ps1
```

如果脚本报告 `Docker Compose command failed`，真正的 Docker 错误会出现在该异常之前，
并且脚本会标明失败阶段（`build`、启动基础设施、`migrate` 或启动应用）。
也可以直接执行以下命令获取完整输出：

```powershell
docker compose -f deploy/docker-compose-dev.yml config
docker compose -f deploy/docker-compose-dev.yml build
```

常见原因：

- Docker Desktop 未启动，或当前终端找不到 `docker.exe`；
- 首次构建拉取 `golang:1.26-alpine` 或 Go 依赖时网络不可用；
- `9999`、`8848` 等端口已被其他程序占用；
- Docker Desktop 的磁盘空间不足。

查看具体服务状态和日志：

```powershell
.\deploy\dev.ps1 -Command ps
docker compose -f deploy/docker-compose-dev.yml logs devops-id devops-system
```
