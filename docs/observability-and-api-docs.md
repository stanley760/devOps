# 接口文档、全链路追踪与监控可视化接入方案

面向开发者的落地手册：在本项目中接入 **① OpenAPI 接口文档（Huma）**、**② OpenTelemetry 全链路追踪**、**③ Prometheus + Grafana 监控可视化**。方案原则与现有架构对齐：

- **能力下沉到 `devops-infra`**：三个新包（`observability`、`http/apidoc`）承载全部通用装配，业务服务只做配置和一行接线（与 `http.Run`/`grpc.Run`/`logging.Init` 同风格）。
- **可观测性单一管线**：埋点统一走 OpenTelemetry API 一次，trace 走 OTLP 出口，metrics 经 Prometheus exporter 被抓取，避免 trace 一套、metrics 一套的双重埋点。
- **零破坏接入**：`http.Run`/`grpc.Run` 现有签名不变（gRPC 仅追加可选字段），业务路由、响应信封、租户/JWT 链路不受影响。

```
                         ┌────────────────────────────┐
                         │  Higress 网关（traceparent 传播）│
                         └──────────┬─────────────────┘
                                    ▼
┌────────────────────────── devops-system / devops-id ─────────────────────────┐
│  otelgin / otelgrpc(自动抽取上游 traceparent)                                │
│   ├─ trace:  OTLP HTTP ──────────────────────▶ Jaeger / Tempo (16686)       │
│   ├─ metric: Prometheus exporter ◀── Prometheus 抓取 /metrics ─▶ Grafana     │
│   └─ log:    slog（自动附加 trace_id/span_id，与 trace 互查）                │
│  GORM(otelgorm) / Redis(redisotel) 出口自动建子 span                         │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 目录

1. [依赖清单](#1-依赖清单)
2. [Part 1：接口文档（Huma）](#2-part-1接口文档Huma)
3. [Part 2：OpenTelemetry 全链路追踪](#3-part-2opentelemetry-全链路追踪)
4. [Part 3：Metrics + Prometheus + Grafana](#4-part-3metrics--prometheus--grafana)
5. [服务装配（devops-system 接线）](#5-服务装配)
6. [部署清单（compose / k8s / Grafana）](#6-部署清单)
7. [安全与最佳实践](#7-安全与最佳实践)
8. [分阶段落地 checklist](#8-分阶段落地-checklist)

---

## 1. 依赖清单

```bash
# —— devops-infra ——
go get go.opentelemetry.io/otel \
       go.opentelemetry.io/otel/sdk \
       go.opentelemetry.io/otel/sdk/metric \
       go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlphttp \
       go.opentelemetry.io/otel/exporters/prometheus \
       go.opentelemetry.io/otel/propagation
go get go.opentelemetry.io/contrib/instrumentors/github.com/gin-gonic/gin/otelgin \
       go.opentelemetry.io/contrib/instrumentors/google.golang.org/grpc/otelgrpc
go get github.com/prometheus/client_golang/prometheus
# OTel SDK 与 contrib 版本需配套（如 sdk v1.3x ↔ contrib v0.5x），一次 go get 引入即可

# —— 接口文档（devops-infra / devops-system）——
go get github.com/danielgtaylor/huma/v2 \
       github.com/danielgtaylor/huma/v2/adapters/humagin   # Gin 适配器，现有路由零替换

# —— 各业务服务 ——
go get gorm.io/plugin/opentelemetry                    # GORM trace 插件
go get github.com/redis/go-redis/extra/redisotel/v9    # Redis trace+metrics 钩子
go get go.opentelemetry.io/contrib/instrumentors/net/http/otelhttp  # 出口 HTTP 传播
```

选型结论：

| 需求 | 选型 | 理由 |
| --- | --- | --- |
| 接口文档 | `huma/v2` + `humagin` 适配器：结构体标签即 OpenAPI 3.1，自带文档 UI 与类型安全校验，零注解零代码生成 | 文档即代码（schema 由 Go 类型推导），无需 swag 注解、`swag init` 与 CI 同步校验；直接跑在现有 Gin 路由上，迁移成本低；内置 docs UI，无需额外 knife4j |
| 追踪 | OTel SDK → OTLP HTTP → **Jaeger all-in-one**（生产可换 Grafana Tempo） | 单二进制开箱即用（4318 OTLP + 16686 UI）；网关 Higress 已可传播 W3C `traceparent` |
| 指标 | OTel Meter + **Prometheus exporter** → Prometheus 抓取 → Grafana | 与 trace 共用一套埋点；`/metrics` 端点由 exporter 提供原生 Prometheus 文本 |
| 日志关联 | slog Handler 包装，自动附加 `trace_id`/`span_id` | 现有 `logging` 包零改动，日志 ↔ trace 互查 |

---

## 2. Part 1：接口文档（Huma）

[Huma](https://huma.rocks) 是一个现代化的 Go API 框架，专注于 OpenAPI 3.1 与开发者体验：

✅ 基于 OpenAPI 3.1 规范，**由 Go 结构体标签直接推导 schema**，无注解、无代码生成步骤
✅ 自带交互式文档 UI（Stoplight Elements 风格），无需额外引入 knife4j
✅ 类型安全的请求/响应校验（required、范围、正则、枚举等）
✅ 适配 Gin/Chi/Fiber 等现有路由，**业务路由零替换**，可逐个接口渐进迁移
✅ 生效路径即文档：`huma.Register(api, ...)` 一处注册，路由 + 校验 + 文档三合一

与 swaggo 方案的本质区别：**不再需要 `swag init` 生成步骤与 `docs` 目录同步维护**——启动服务即得到实时 OpenAPI JSON（`/apidoc/openapi.json`）与 UI（`/apidoc`）。

### 2.1 基础装配：`devops-infra/http/apidoc`（新包）

```go
// Package apidoc 基于 huma/v2 提供统一 API 装配：注册huma API 到现有 Gin 路由。
package apidoc

import (
    "context"
    "net/http"

    "github.com/danielgtaylor/huma/v2"
    "github.com/danielgtaylor/huma/v2/adapters/humagin"
    "github.com/gin-gonic/gin"
)

type Options struct {
    Title       string // 文档标题，如 "devops-system API"
    Version     string // 缺省 "v1"
    BasePath    string // 文档挂载前缀，默认 /apidoc
    OpenAPIPath string // OpenAPI JSON 路径，默认 /apidoc/openapi.json
}

// Mount 在现有 Gin 路由上创建 huma API 并挂载文档 UI；
// 返回 *huma.API，业务侧通过 huma.Register(api, ...) 注册接口。
func Mount(r gin.IRouter, opt Options) huma.API {
    if opt.BasePath == "" {
        opt.BasePath = "/apidoc"
    }
    if opt.OpenAPIPath == "" {
        opt.OpenAPIPath = "/apidoc/openapi.json"
    }
    api := humagin.New(r, huma.DefaultConfig(opt.Title, opt.Version))
    huma.Register(api, huma.Operation{
        OperationID: "openapi-json",
        Method:      http.MethodGet,
        Path:        opt.OpenAPIPath,
        Summary:     "导出 OpenAPI 3.1 文档",
    }, func(ctx context.Context, _ struct{}) (huma.Status200, *huma.OpenAPI) {
        return http.StatusOK, api.OpenAPI()
    })
    // huma 自带 docs UI（路由 opt.BasePath），对外即 /apidoc
    return api
}
```

### 2.2 业务接口注册：结构体即文档（`devops-system/internal/api/v1`）

以「创建用户」为例，请求/响应结构体直接作为文档 schema，信封与现有 `code/data/msg` 保持一致：

```go
// —— 定义在 handler 包顶部的 schema 结构体 ——

type CreateUserInput struct {
    Body struct {
        Username string `json:"username" minLength:"3" maxLength:"32" doc:"登录名"`
        Email    string `json:"email" format:"email" doc:"邮箱"`
        RoleIDs  []int64 `json:"role_ids" minItems:"0" doc:"角色ID列表"`
    }
}

type Envelope[T any] struct {
    Code int    `json:"code" enum:"0" doc:"0=成功"`
    Msg  string `json:"msg"`
    Data T      `json:"data"`
}

type CreateUserOutput struct {
    Body Envelope[struct {
        ID       int64  `json:"id"`
        Username string `json:"username"`
        Email    string `json:"email"`
    }]
}

// —— 在 Register 回调中注册（与现有路由组复用同一 gin engine）——
func RegisterUser(api huma.API, h *Handler) {
    huma.Register(api, huma.Operation{
        OperationID: "create-user",
        Method:      http.MethodPost,
        Path:        "/api/v1/users",
        Summary:     "创建用户",
        Tags:        []string{"users"},
        Security:    []map[string][]string{{"bearerAuth": {}}}, // 文档标注 JWT
    }, func(ctx context.Context, in *CreateUserInput) (*CreateUserOutput, error) {
        u, err := h.CreateUser(ctx, in.Body.Username, in.Body.Email) // 校验已由 huma 完成
        if err != nil {
            return nil, errs.ToHumaError(err) // 业务错误→RFC 7807 Problem Details
        }
        return &CreateUserOutput{Body: Envelope[...]{
            Code: 0, Msg: "ok",
            Data: struct {
                ID int64 `json:"id"`
                Username string `json:"username"`
                Email string `json:"email"`
            }{ID: u.ID, Username: u.Username, Email: u.Email},
        }}, nil
    })
}
```

> 安全声明（JWT Bearer）在创建 API 后统一追加：
> `api.OpenAPI().Security = ...` + `api.OpenAPI().Components.SecuritySchemes["bearerAuth"] = ...`。

### 2.3 渐进迁移策略

- huma 的 `humagin` 适配器直接复用现有 `gin.Engine`，**旧 handler 不必一次性改写**：先 `apidoc.Mount` 拿到 `huma.API`，新接口一律走 `huma.Register`；旧接口按迭代节奏逐步迁移（humagin 支持 `humagin.Wrap` 将已有 gin handler 包装为 huma 操作补文档）。
- 租户头 `X-Tenant-ID` 等公共参数可定义在公共 input 结构体或通过 operation middleware 注入，文档与校验一次到位。
- 校验失败/业务错误统一转为 huma 的 Problem Details 响应（`errs` 包加 `ToHumaError`），与现有信封兼容层在网关/前端侧适配。

---

## 3. Part 2：OpenTelemetry 全链路追踪

### 3.1 核心装配：`devops-infra/observability`（新包）

```go
// Package observability 统一装配 OpenTelemetry：trace（OTLP）+ metrics（Prometheus）+ 日志关联。
package observability

import (
    "context"
    "fmt"
    "time"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlphttp"
    "go.opentelemetry.io/otel/exporters/prometheus"
    "go.opentelemetry.io/otel/propagation"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Config struct {
    ServiceName    string  // 服务名（缺省取 nacos.registry.service_name）
    Env            string  // deployment.environment（dev/test/prod）
    OTLPEndpoint   string  // 如 "http://jaeger:4318"；空 = 关闭 trace 出口
    SamplerRatio   float64 // trace 采样率，prod 建议 0.1
    MetricsEnabled bool    // 是否启用 Prometheus exporter
}

type Runtime struct {
    tracerProvider *sdktrace.TracerProvider
    meterProvider  *sdkmetric.MeterProvider
    MetricsHandler http.Handler // 非 nil 时挂到 /metrics
}

// Init 装配全局 OTel；应在 logging.Init 之后、任何请求处理之前调用。
func Init(ctx context.Context, cfg Config) (*Runtime, error) {
    res, err := resource.New(ctx,
        resource.WithFromEnv(), resource.WithProcessRuntimeDescription(),
        resource.WithAttributes(
            semconv.ServiceName(cfg.ServiceName),
            semconv.DeploymentEnvironmentName(cfg.Env),
        ),
    )
    if err != nil {
        return nil, err
    }
    rt := &Runtime{}
    opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}

    if cfg.OTLPEndpoint != "" {
        exp, err := otlphttp.New(ctx, otlphttp.WithEndpoint(cfg.OTLPEndpoint), otlphttp.WithInsecure())
        if err != nil {
            return nil, fmt.Errorf("创建 OTLP exporter 失败: %w", err)
        }
        // ParentBased：上游带采样决定时尊重上游；根 span 按比例采样
        opts = append(opts,
            sdktrace.WithBatcher(exp, sdktrace.WithMaxQueueSize(8192), sdktrace.WithBatchTimeout(5*time.Second)),
            sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerRatio))),
        )
    }
    rt.tracerProvider = sdktrace.NewTracerProvider(opts...)

    if cfg.MetricsEnabled {
        promExp, err := prometheus.New() // 内置默认 registry 包装
        if err != nil {
            return nil, err
        }
        rt.meterProvider = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res),
            sdkmetric.WithReader(promExp))
        rt.MetricsHandler = promHandler(promExp) // promhttp 包装，见 4.1
    }

    otel.SetTracerProvider(rt.tracerProvider)
    if rt.meterProvider != nil {
        otel.SetMeterProvider(rt.meterProvider)
    }
    // W3C traceparent：与 Higress/前端/跨服务传播对齐
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{}, propagation.Baggage{}))
    return rt, nil
}

// Shutdown 优雅关闭：flush 在途 trace/metrics（进程退出前调用）。
func (rt *Runtime) Shutdown(ctx context.Context) error {
    var firstErr error
    if rt.meterProvider != nil {
        if err := rt.meterProvider.Shutdown(ctx); err != nil && firstErr == nil { firstErr = err }
    }
    if rt.tracerProvider != nil {
        if err := rt.tracerProvider.Shutdown(ctx); err != nil && firstErr == nil { firstErr = err }
    }
    return firstErr
}
```

### 3.2 Gin 入口埋点（`observability/gin.go`）

```go
// Middleware 返回 gin 入口埋点：提取上游 traceparent → 建 server span → 注入 route 属性。
func Middleware(serviceName string) gin.HandlerFunc {
    return otelgin.Middleware(serviceName) // otelgin 自带 W3C extract/inject 与 404 过滤
}
```

在业务路由注册回调**最前**挂载（先于业务中间件，使 JWT/tenant/perm 的耗时都落进同一个 server span 的子结构）：

```go
r.Use(obs.Middleware("devops-system"))   // obs *observability.Runtime 可为 nil 判断后跳过
```

### 3.3 gRPC 埋点（推荐 stats.Handler 方式）

`grpc.Run` 的 `ServerConfig` 追加可选字段（additive，现有调用不受影响）：

```go
type ServerConfig struct {
    Listen            string
    Reflection        bool
    StatsHandler      grpc.StatsHandler            // 新增：OTel
    UnaryInterceptors []grpc.UnaryServerInterceptor // 新增：自定义链
}

// Run 内组装：
opts := []grpc.ServerOption{}
if cfg.StatsHandler != nil {
    opts = append(opts, grpc.StatsHandler(cfg.StatsHandler))
}
s := grpc.NewServer(append(opts,
    grpc.ChainUnaryInterceptor(append([]grpc.UnaryServerInterceptor{
        interceptor.Recovery(), interceptor.AccessLog(),
    }, cfg.UnaryInterceptors...)...))...)
```

服务侧装配（devops-id 与各客户端同款）：

```go
grpcinfra.Run(grpcinfra.ServerConfig{
    Listen:       ":9090",
    StatsHandler: otelgrpc.NewServerHandler(), // trace + RED metrics 一次到位
}, register)

// 客户端（cmd/root.go 的 Dial 处追加）：
conn, err := grpc.NewClient(endpoint,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithStatsHandler(otelgrpc.NewClientHandler()), // 出口传播 traceparent
)
```

### 3.4 GORM / Redis 出口埋点

```go
// MySQL（database/mysql client 初始化后）：
_ = gdb.Use(otelgorm.NewPlugin(otelgorm.WithDBName("devops")))  // gorm.io/plugin/opentelemetry

// Redis（cache/redis client 初始化后）：
_ = redisotel.InstrumentTracing(rdb)   // 每条命令建子 span
_ = redisotel.InstrumentMetrics(rdb)   // 命令耗时/错误指标
```

此后每个请求的 trace 树形如：`GET /api/v1/users/:id` → `auth.Enforce(casbin/redis)` → `gorm.users.Query` → `redis.cmd`，DB/Redis 慢查询一眼定位。

### 3.5 日志关联 trace（slog 零侵入包装）

```go
// observability/log.go
type TraceHandler struct{ slog.Handler }

func (h TraceHandler) Handle(ctx context.Context, r slog.Record) error {
    if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
        r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
    }
    return h.Handler.Handle(ctx, r)
}
```

在 `logging.Init(cfg.Log)` 之后一行启用，**中间件与 handler 内已有 slog 调用自动携带 trace_id**（它们的第一个参数都是 `c.Request.Context()`）：

```go
slog.SetDefault(slog.New(observability.TraceHandler{Handler: slog.Default().Handler()}))
```

### 3.6 跨服务传播规则

| 调用形态 | 传播方式 | 需要做的 |
| --- | --- | --- |
| 网关 → 服务 | W3C `traceparent` 头 | otelgin 自动 extract，无需代码 |
| 服务 → 服务（gRPC） | metadata `traceparent` | 双端加 `otelgrpc` stats handler |
| 服务 → 服务（HTTP） | `traceparent` 头 | 出口 `otelhttp.NewTransport(baseTransport)` |
| 异步任务/定时任务 | ctx 裸传 | 从上游消息头重建：`propagator.Extract(ctx, carrier)` |

---

## 4. Part 3：Metrics + Prometheus + Grafana

### 4.1 RED 指标中间件（`observability/metrics.go`）

用 OTel Meter API 定义指标（经 Prometheus exporter 原生输出），HTTP 与 gRPC 同口径：

```go
var (
    meter           = otel.Meter("devops-infra/observability")
    httpRequests, _ = meter.Int64Counter("http_server_requests_total",
        instrument.WithDescription("HTTP 请求总数"))
    httpDuration, _ = meter.Float64Histogram("http_server_request_duration_seconds",
        instrument.WithDescription("HTTP 请求耗时（秒）"), instrument.WithUnit("s"))
    httpInFlight, _ = meter.Int64UpDownCounter("http_server_in_flight",
        instrument.WithDescription("进行中请求数"))
)

// MetricsMiddleware RED 埋点；建议挂在 otelgin 之后、AccessLog 之前。
func MetricsMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        httpInFlight.Add(c.Request.Context(), 1)
        start := time.Now()
        c.Next()
        attrs := metric.WithAttributes(
            attribute.String("method", c.Request.Method),
            attribute.String("route", routeTemplate(c)), // c.FullPath()，404 归 "unmatched"
            attribute.Int("status", c.Writer.Status()),
        )
        httpRequests.Add(c.Request.Context(), 1, attrs)
        httpDuration.Record(c.Request.Context(), time.Since(start).Seconds(), attrs)
        httpInFlight.Add(c.Request.Context(), -1)
    }
}
```

`/metrics` 端点直接复用 `Runtime.MetricsHandler`（promhttp 包装 exporter collector），在路由回调公开挂载：

```go
if obs != nil && obs.MetricsHandler != nil {
    r.GET("/metrics", gin.WrapH(obs.MetricsHandler))   // 公开路由：JWT 之前，/healthz 同级
}
```

> gRPC 侧指标由 `otelgrpc` stats handler 自动产出（`rpc_server_requests_total` 等），无需手写。

### 4.2 Prometheus 抓取

**docker-compose（dev）**：`deploy/prometheus/prometheus.yml`

```yaml
global:
  scrape_interval: 15s
scrape_configs:
  - job_name: devops-services
    static_configs:
      - targets: ["devops-system:8080", "devops-id:9091"]  # HTTP 端口；gRPC 服务可开独立 metrics 端口
    metrics_path: /metrics
  - job_name: prometheus
    static_configs: [{ targets: ["localhost:9090"] }]
```

**k8s（prod）**：在 Deployment pod template 加注解即可被 Prometheus 抓取（或使用 prometheus-operator 的 ServiceMonitor）：

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port:   "8080"
  prometheus.io/path:   "/metrics"
```

### 4.3 Grafana 面板（RED 三件套）

核心 PromQL（`service_name` 标签来自 OTel Resource）：

```promql
# QPS
sum(rate(http_server_requests_total{service_name="devops-system"}[1m])) by (route)

# 错误率（5xx 占比）
sum(rate(http_server_requests_total{service_name="devops-system", status=~"5.."}[1m]))
  / sum(rate(http_server_requests_total{service_name="devops-system"}[1m]))

# P95 / P99 延迟
histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket[5m])) by (le, route))
histogram_quantile(0.95, sum(rate(http_server_request_duration_seconds_bucket[5m])) by (le, route))

# 在途请求 / DB-Redis 观察
http_server_in_flight
rpc_server_requests_total{rpc_grpc_status_code!="OK"}
```

Grafana provisioning（`deploy/grafana/provisioning/datasources/`）一次性配置 Prometheus + Jaeger 双数据源，并在面板里配置 trace ↔ metrics 互跳链接：

```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    url: http://prometheus:9090
    isDefault: true
  - name: Jaeger
    type: jaeger
    url: http://jaeger:16686
    jsonData:
      tracesToMetrics:
        datasourceUid: prometheus   # 从 span 跳到对应指标面板
```

面板 JSON 建议放 `deploy/grafana/provisioning/dashboards/` 由 provisioning 自动加载；起步可直接导入 Grafana 社区的 Go/RED 模板再按上面 PromQL 调整。

---

## 5. 服务装配

### 5.1 配置新增（`devops-system/internal/config/config.go` + YAML）

```go
type ApiDocConfig struct {
    Enabled  bool   `mapstructure:"enabled"`   // dev/test true，prod false
    BasePath string `mapstructure:"base_path"` // 默认 /apidoc
}

type ObservabilityConfig struct {
    OTLPEndpoint   string  `mapstructure:"otlp_endpoint"`   // http://jaeger:4318，空=关
    SamplerRatio   float64 `mapstructure:"sampler_ratio"`   // 默认 1.0；prod 0.1
    MetricsEnabled bool    `mapstructure:"metrics_enabled"` // /metrics 开关
}
```

```yaml
apidoc:
  enabled: true
  base_path: /apidoc
observability:
  otlp_endpoint: "http://jaeger:4318"
  sampler_ratio: 1.0
  metrics_enabled: true
```

### 5.2 `internal/cmd/root.go` 接线（对齐现有装配链）

```go
PersistentPreRunE: func(...) error {
    // ...现有 conf/nacos/logging 之后：
    obs, err := observability.Init(cmd.Context(), observability.Config{
        ServiceName: cfg.Nacos.Registry.ServiceName,
        Env:         infracfg.Env(),
        OTLPEndpoint: cfg.Obsv.OTLPEndpoint,
        SamplerRatio: cfg.Obsv.SamplerRatio,
        MetricsEnabled: cfg.Obsv.MetricsEnabled,
    })
    if err != nil { return err }
    slog.SetDefault(slog.New(observability.TraceHandler{Handler: slog.Default().Handler()}))
    // obs 存入包级变量供 RunE 与 router 使用
}

RunE: func(...) error {
    // GORM/Redis 埋点（初始化后）：
    // _ = gdb.Use(otelgorm.NewPlugin(...)); _ = redisotel.InstrumentTracing(store.Get())
    // gRPC Dial 追加 otelgrpc.NewClientHandler()
    err = httpinfra.Run(cfg.Server, router.Register(..., obs), readiness...)
}

PersistentPostRunE: func(...) error {
    // 现有关闭链之后：
    _ = obs.Shutdown(context.Background())   // flush 在途 trace
    return logging.Close()
}
```

`router.Register` 签名追加 `obs *observability.Runtime`，回调最前挂三个中间件/端点（都在认证组之前）：

```go
r.Use(obs.Middleware("devops-system"))   // trace
r.Use(observability.MetricsMiddleware()) // RED
r.GET("/metrics", gin.WrapH(obs.MetricsHandler))
if cfg.ApiDoc.Enabled {
    api := apidoc.Mount(r, apidoc.Options{Title: "devops-system API"})
    v1.RegisterUsers(api, userHandler) // 新接口huma.Register，旧接口渐进迁移
}
```

最终 HTTP 中间件链：

```
otelgin(trace) → MetricsMiddleware(RED) → gin.Logger → Recovery → AccessLog
  ├─ 公开：/healthz /readyz /metrics /apidoc/* /auth/login /auth/refresh /jwks
  └─ JWT → Tenant → Perm → handler
```

---

## 6. 部署清单

### 6.1 `deploy/docker-compose-dev.yml` 追加

```yaml
  jaeger:
    image: jaegertracing/all-in-one:1.62.0
    environment:
      - COLLECTOR_OTLP_ENABLED=true
    ports:
      - "4317:4317"   # OTLP gRPC
      - "4318:4318"   # OTLP HTTP（服务出口）
      - "16686:16686" # Jaeger UI
  prometheus:
    image: prom/prometheus:v2.55.1
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports:
      - "9090:9090"
  grafana:
    image: grafana/grafana:11.3.0
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin   # dev 默认，prod 接 SSO/密管
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - dev-grafana-data:/var/lib/grafana
    ports:
      - "3000:3000"
```

对应服务环境变量：`devops-system` 增加 `OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318`（或直接走上面的 `observability.otlp_endpoint` 配置）。

### 6.2 k8s（`deploy/k8s`）

- Deployment：加 4.2 的 scrape 注解 + `OTEL_EXPORTER_OTLP_ENDPOINT` env（指向集群内 Jaeger/Tempo/Collector Service）；
- 大流量场景在服务与后端之间加 **otel-collector**（队列缓冲、尾部采样、敏感字段过滤），服务端点改指 collector；
- Jaeger/Tempo 与 Grafana 建议按环境独立部署，采集端 `deployment.environment=prod` 已随 Resource 上报，可在 Grafana 里按环境过滤。

---

## 7. 安全与最佳实践

| 项 | 约定 |
| --- | --- |
| `/apidoc` | dev/test 开、**prod 关**（`apidoc.enabled=false`）；确需常开则加 BasicAuth/内网 ingress 限制 |
| `/metrics` | 与 `/healthz` 同级公开端口即可，但 **不要经 Higress 暴露到公网**（ingress 不路由 `/metrics` `/apidoc`），k8s 场景优先 NetworkPolicy 限内网 |
| 采样 | dev `sampler_ratio=1.0` 全量；prod `0.1` 起步，问题排查临时调高；头部采样不够时上 collector 尾部采样 |
| 标签基数 | metrics 属性只用 `method/route/status` 这类**有界**值：route 取 `FullPath()` 模板而非原始 path（防 :id 爆基数）；禁止把 uid/tid 放指标标签 |
| 资源属性 | `service.name` 必须与 nacos 注册名一致，跨服务 trace 与 Grafana 服务过滤都依赖它 |
| 日志 | trace_id 自动注入后，排查动线固定为：Grafana 指标异常 → 复制时间窗 → Jaeger 按 service+时间查慢 trace → 复制 trace_id 搜业务日志 |
| 版本 | OTel SDK 与 contrib 同步升级；prometheus exporter 指标命名已按 Prometheus 规范转换，Grafana 面板引用升级日志确认无改名 |

---

## 8. 分阶段落地 checklist

**阶段 1：接口文档（半天）**
- [ ] 新增 `devops-infra/http/apidoc`：`huma.DefaultConfig` + `humagin.New` 封装 `Mount`，在 `router.Register` 公开挂载（`apidoc.enabled` 门控）
- [ ] 新接口全部改用 `huma.Register` 注册：结构体标签即文档（`json/minLength/format/doc` 等），信封 `Envelope[T]` 泛型复用
- [ ] 旧 handler 用 `humagin.Wrap` 包装补文档，按迭代渐进迁移；`errs` 包加 `ToHumaError` 转 Problem Details
- [ ] JWT Bearer / `X-Tenant-ID` 在 OpenAPI Components 中声明，文档页可自助调试（带 token 试运行）
- [ ] 前端按 `/apidoc/openapi.json` 对接：信封 code、`X-Tenant-ID`、Bearer 三件事在文档页即可自助查

**阶段 2：全链路追踪（1 天）**
- [ ] 新增 `devops-infra/observability`（Init/Shutdown/Middleware/TraceHandler）
- [ ] `grpc.ServerConfig` 加 `StatsHandler/UnaryInterceptors` 字段；devops-id 服务端与各客户端接 otelgrpc
- [ ] GORM/Redis 插件埋点；`root.go` 装配与 Shutdown；compose 引入 jaeger
- [ ] 验证：登录 → 列表 → 详情一条完整 trace，含 gRPC 跨服务与 Redis/GORM 子 span；日志含 trace_id

**阶段 3：指标与可视化（半天）**
- [ ] RED 中间件 + `/metrics` 挂载；Prometheus 抓取配置
- [ ] Grafana datasource provisioning + RED 面板（QPS/错误率/P99/在途）
- [ ] 验证压测：面板数字与 `/metrics` 原始值一致，5xx 报警联动

**阶段 4（可选增强）**
- [ ] otel-collector（尾部采样/PII 过滤）；trace ↔ metrics 互跳 exemplar
- [ ] Higress 网关侧 tracing 接入，端到端含网关 span
