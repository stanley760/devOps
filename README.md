# devOps

![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)
![Gin](https://img.shields.io/badge/Gin-v1.12-00ADD8?logo=gin)
![Cobra](https://img.shields.io/badge/Cobra-v1.10-00ADD8)
![Viper](https://img.shields.io/badge/Viper-v1.21-00ADD8)

基于 Go 的运维服务基础框架，提供多环境配置、结构化异步日志、统一响应与错误处理、优雅退出等开箱即用的工程能力。

A DevOps service foundation built with Go, featuring multi-environment configuration, structured async logging, unified response/error handling, and graceful shutdown.

## 特性 / Features

- **多环境配置**：`config.yaml` 基础配置 + `APP_ENV` 按环境覆盖（dev/test/prod），无需改动代码即可切换环境
- **统一响应**：所有接口返回 `BaseResponse` 信封 `{"code", "msg", "data"}`，业务码与 HTTP 状态码一致
- **统一错误处理**：业务层返回 `error`，由 `response.Wrap` 适配器统一转换；`BizError` 携带业务码与用户可读信息，未知错误细节只进日志不泄露
- **结构化异步日志**：slog + 异步缓冲写入 + 按大小/时间双维度滚动 + gzip/zstd 压缩，对齐 logback 的 `SizeAndTimeBasedRollingPolicy`
- **生产就绪**：panic 恢复、统一 404/405、可信代理配置、SIGINT/SIGTERM 优雅退出（10s 排空）
- **CLI 化服务**：Cobra 子命令架构（`devops server`），版本号从 Git 自动注入

## 目录 / TOC

- [快速开始](#快速开始--quick-start)
- [命令行 / CLI](#命令行--cli)
- [多环境配置 / Configuration](#多环境配置--configuration)
- [项目结构 / Project Structure](#项目结构--project-structure)
- [HTTP 服务 / HTTP API](#http-服务--http-api)
- [统一响应与错误处理 / Response & Errors](#统一响应与错误处理--response--errors)
- [日志 / Logging](#日志--logging)
- [可信代理 / Trusted Proxies](#可信代理--trusted-proxies)
- [开发指南 / Development](#开发指南--development)
- [测试 / Testing](#测试--testing)
- [后续规划 / Roadmap](#后续规划--roadmap)

## 快速开始 / Quick Start

### 环境要求

- Go 1.26+
- （可选）Git —— 用于自动嵌入版本信息

### 运行

```powershell
# PowerShell
$env:APP_ENV = "dev"; go run . server
```

```bash
# bash
APP_ENV=dev go run . server
```

```text
     _
  __| | _____   _____  _ __  ___
 / _` |/ _ \ \ / / _ \| '_ \/ __|
| (_| |  __/\ V / (_) | |_) \__ \
 \__,_|\___| \_/ \___/| .__/|___/
                      |_|
 Service Version: v0.1.0
...
time=... level=INFO msg="HTTP 服务启动" addr=:8080 mode=debug
```

### 构建发布

```powershell
# Windows：使用构建脚本，版本号取自 git describe
./build.ps1          # → bin/devops.exe
```

```bash
# 或手动构建
go build -o bin/devops .
```

### 验证

```powershell
curl.exe http://localhost:8080/healthz
# {"code":200,"msg":"ok","data":{"status":"ok"}}
```

## 命令行 / CLI

```text
devops [flags] [command]
```

| 命令 | 说明 |
|---|---|
| `devops server` | 启动 HTTP 服务 |
| `devops help` | 查看帮助 |
| `devops --version` | 查看版本 |

| 全局参数 | 说明 |
|---|---|
| `-f, --config-file` | 配置文件路径，默认 `config/config.yaml` |
| `-v, --version` | 版本号 |

**版本号解析优先级**（见 `main.go` 的 `resolveVersion`）：

1. 构建时 `-ldflags "-X main.version=xxx"` 注入（`build.ps1` 使用 `git describe --tags --always --dirty`）
2. 模块版本（从模块代理安装时非 `(devel)`)
3. `vcs.revision` 短哈希 + `-dirty` 标记（仓库内 `go build` 自动嵌入）
4. 兜底 `unknown`（`go run` 不嵌入 VCS 信息，属预期行为；开发期可 `go run -ldflags "-X main.version=dev" .`）

## 多环境配置 / Configuration

配置采用「基础 + 覆盖」模式：

- `config/config.yaml` —— 基础配置，写全公共项
- `config/{dev,test,prod}.yaml` —— 仅覆盖与基础配置不同的字段，未写的继承基础值

通过环境变量 `APP_ENV` 选择环境，未设置时只加载基础配置：

```powershell
$env:APP_ENV = "prod"; .\bin\devops.exe server
```

```bash
APP_ENV=prod ./devops server
```

### `server` 段

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `port` | `8080` | 监听端口 |
| `read_timeout` | `10s` | 读超时，`0` 不限制 |
| `write_timeout` | `10s` | 写超时，`0` 不限制 |
| `trusted_proxies` | `[127.0.0.1, ::1]` | 可信反向代理 IP/CIDR，见[可信代理](#可信代理--trusted-proxies) |

### `log` 段

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `level` | `debug` | 日志级别：debug / info / warn / error |
| `dir` | `logs` | 日志目录，不存在自动创建 |
| `console` | `true` | 是否同时输出到控制台 |
| `format` | `text` | 文件格式：text（易读）/ json（便于采集） |
| `buffer` | `4096` | 异步缓冲区大小（条），写满丢弃新日志避免阻塞业务 |
| `max_size` | `100` | 单文件上限（MB），写满立即切分 |
| `max_backups` | `30` | 保留历史文件个数，`0` 不限制 |
| `max_age` | `7` | 历史文件保留天数，`0` 不限制 |
| `compress` | `gzip` | 旧文件压缩：none / gzip / zstd |
| `rotation_interval` | `24h` | 时间滚动间隔，留空或 `0` 仅按大小滚动 |

### 环境差异示例

`dev.yaml`（切分粒度小、不压缩，方便观察）：

```yaml
log:
  max_size: 50
  max_backups: 10
  max_age: 3
  compress: none
```

`prod.yaml`（级别收敛、JSON 结构化、大缓冲）：

```yaml
log:
  level: info
  console: false
  format: json
  buffer: 16384
  max_size: 200
  max_backups: 60
  max_age: 15
```

启动时日志会打印实际生效的配置来源，例如 `config=config/config.yaml + dev.yaml`。

## 项目结构 / Project Structure

```text
devOps/
├── main.go                    # 入口：banner、版本解析、cmd.Execute
├── build.ps1                  # 构建脚本（版本号注入）
├── cmd/                       # CLI 层
│   ├── root.go                #   根命令：配置加载（OnInitialize）→ 组件初始化（PersistentPreRunE）
│   ├── server/server.go       #   server 子命令：启动 HTTP 服务
│   └── initialize/            #   统一初始化：配置 → 日志（未来扩展 DB/Redis）
├── config/                    # 配置文件（yaml）
│   ├── config.yaml            #   基础配置
│   └── dev|test|prod.yaml     #   环境覆盖
└── internal/
    ├── config/                # 配置结构体 + viper 加载/环境合并/热更新
    ├── errs/                  # 错误码 + BizError 业务错误类型
    ├── logger/                # slog 异步日志 + timberjack 滚动
    ├── middleware/            # Recovery（panic 统一 500）/ AccessLog（访问日志）
    ├── response/              # BaseResponse 统一响应 + Wrap 错误适配器
    └── router/                # 路由组装、优雅退出
```

**分层调用链**：`cmd → router → (api → service → dao) → model`；基础设施组件（logger 及未来的 db/redis/authz）用包级 `Init/Close` 单例，业务层构造注入。

## HTTP 服务 / HTTP API

| 方法 | 路径 | 说明 | 鉴权 |
|---|---|---|---|
| GET | `/healthz` | 健康检查 | 无 |

> 业务路由（`/api/v1`）随权限系统逐步开放，见 [Roadmap](#后续规划--roadmap)。

统一中间件链：`gin.Logger()`（控制台请求日志）→ `Recovery`（panic 统一处理）→ `AccessLog`（slog 访问日志）。

- 未匹配路由 → `404` 统一信封；未允许方法 → `405` 统一信封
- 收到 `SIGINT/SIGTERM` 后优雅退出，10 秒内排空在途请求
- gin 运行模式随环境切换：`APP_ENV` 为空或 `dev` 用 debug（启动打印路由表），其余用 release

## 统一响应与错误处理 / Response & Errors

所有接口返回统一信封，`code` 与 HTTP 状态码一致：

```json
// 成功
{"code": 200, "msg": "ok", "data": {"id": 1}}

// 分页（response.OKPage）
{"code": 200, "msg": "ok", "data": {"list": [...], "total": 42}}

// 失败
{"code": 404, "msg": "接口不存在", "data": null}
```

| code | HTTP 语义 | 场景 |
|---|---|---|
| 200 | OK | 成功 |
| 400 | Bad Request | 参数错误 |
| 401 | Unauthorized | 未登录或会话已过期 |
| 403 | Forbidden | 无操作权限 |
| 404 | Not Found | 资源不存在 / 接口不存在 |
| 405 | Method Not Allowed | 请求方法不支持 |
| 500 | Internal Server Error | 服务器内部错误 |

### 业务 handler 编写范式

业务 handler 签名为 `func(c *gin.Context) error`：成功路径调用 `OK / OKPage` 写响应，失败路径直接返回 `error`，由 `response.Wrap` 统一转换，不在业务代码里手写错误响应：

```go
r.GET("/api/v1/users/:id", response.Wrap(func(c *gin.Context) error {
    user, err := svc.Get(c.Param("id"))
    if err != nil {
        return err // *errs.BizError 取其 code/msg；未知错误 500 且细节只进日志
    }
    response.OK(c, user)
    return nil
}))
```

`Wrap` 的兜底行为：

- 返回 `*errs.BizError`（含被 `fmt.Errorf("%w")` 包装的）→ 按其 code/msg 响应
- 返回其他错误 → 500，内部细节记 slog 不向客户端泄露
- 返回 `nil` 但漏写响应 → 兜底 500，避免请求挂起
- 已写响应又返回错误 → 记日志后中断，不再改写

业务层创建错误：

```go
// 预定义错误可直接返回
return errs.ErrParam

// 带上下文包装（保留底层错误供日志排查）
return errs.Wrap(dbErr, errs.CodeParam, "用户名已存在")
```

## 日志 / Logging

双通道输出：

- **控制台**：gin 请求日志（彩色）+ slog 应用日志
- **文件**：slog 异步写入（缓冲避免磁盘 IO 阻塞业务），text/json 双格式

文件滚动策略对齐 logback 的 `SizeAndTimeBasedRollingPolicy`：按大小（写满即切）与按时间（到点即切）双维度，旧文件可 gzip/zstd 压缩、按个数/天数清理。

```text
logs/
├── devops.log                                 # 当前文件
├── devops-2026-09-10-16-44-05.123-size.log    # 按大小切分的备份
└── ...
```

slog 结构化日志示例：

```text
time=2026-09-11T10:10:51.314+08:00 level=DEBUG msg=初始化完成 config="config/config.yaml + dev.yaml" env=dev logLevel=debug
time=2026-09-11T10:11:02.001+08:00 level=DEBUG msg="HTTP 请求" method=GET path=/healthz status=200 latency=123.4µs bytes=42 client_ip=127.0.0.1
```

进程退出时自动排空缓冲并关闭文件（`PersistentPostRunE` 保证日志不丢）。

## 可信代理 / Trusted Proxies

默认可信代理 `127.0.0.1` / `::1`（nginx 与服务同机部署的场景）：

```yaml
server:
  trusted_proxies:
    - 127.0.0.1
    - ::1
```

服务不在反向代理后面时，建议置为空列表，`c.ClientIP()` 将直连对端地址：

```yaml
server:
  trusted_proxies: []
```

不要配置 `0.0.0.0/0`，任何客户端都可伪造 `X-Forwarded-For`，导致访问日志中的客户端 IP 不可信。

## 开发指南 / Development

### 代码约定

- 注释使用中文；错误用 `fmt.Errorf("%w")` 包装保留原始错误
- 基础设施组件（logger、未来的 db/redis）：包级 `Init/Close` 单例，风格参照 `internal/logger`
- 业务层：struct 构造注入，依赖显式
- 提交前执行：`go build ./...`、`go vet ./...`、`go test ./...`、`gofmt -l .`

### 新增一个 HTTP 接口

1. 在 `internal/router/router.go` 注册路由，用 `response.Wrap` 包装 handler
2. handler 内绑定参数（binding tag）→ 调 service → `response.OK` / `OKPage` 写响应
3. 失败路径返回 `error`：业务失败用 `errs.New / errs.Wrap`，参数校验失败返回 `errs.ErrParam`

### 新增一个子命令

在 `cmd/<name>/` 下创建包，导出 `Cmd`，并在 `cmd/root.go` 的 `Execute` 中 `rootCmd.AddCommand(xxx.Cmd)`。配置加载与组件初始化已由 root 的 `OnInitialize` / `PersistentPreRunE` 统一完成。

### 新增配置项

1. `internal/config/config.go` 对应结构体加字段（`mapstructure` tag）
2. `config/config.yaml` 加默认值并写注释
3. 如有环境差异，在 `config/{env}.yaml` 覆盖

## 测试 / Testing

```powershell
go test ./...
```

当前覆盖：`internal/logger`（异步写入、滚动策略）、`internal/middleware`（panic 恢复）、`internal/response`（信封格式、错误穿透、兜底行为）。

## 后续规划 / Roadmap

- [ ] 用户认证与 RBAC 权限系统（Gin + Casbin + Redis + MySQL：用户/角色/菜单/部门/职位）
- [ ] `/api/v1` 业务路由与分页规范
- [ ] 健康检查细化与 Prometheus 指标接口
- [ ] Docker 与生产环境部署配置
- [ ] 添加 LICENSE 与 CONTRIBUTING
