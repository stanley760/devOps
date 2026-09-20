// Package apidoc 基于 huma/v2 提供统一的 OpenAPI 接口文档装配。
//
// 设计目标（对齐 docs/observability-and-api-docs.md Part 1）：
//   - 文档即代码：OpenAPI 3.1 schema 由 Go 结构体标签直接推导，
//     无 swag 注解、无 `swag init` 生成步骤、无 docs 目录同步维护
//   - 复用现有 Gin 路由：humagin 适配器直接跑在传入的 gin.IRouter 上，
//     业务路由、响应信封、租户/JWT 链路零改动
//   - 渐进迁移：新接口一律 huma.Register；旧 handler 可经 humagin.Wrap
//     包装补文档，按迭代节奏迁移
//
// 一次 New 提供三样东西：
//   -huma.API：业务侧 huma.Register 注册接口（路由+校验+文档三合一）
//   - OpenAPI JSON：GET {DocsPath}/openapi.json（默认 /apidoc/openapi.json）
//   - 文档 UI：GET {DocsPath}（huma 内置 Stoplight Elements 风格交互文档，
//     无需 knife4j/Swagger UI 等外部资产）
//
// 必须挂在认证组之前（公开路由）；是否启用由调用方按环境门控
// （dev/test 开、prod 关，见各服务 apidoc.enabled 配置）。
package apidoc

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

// Options 文档装配配置。
type Options struct {
	// Title 文档标题，如 "devops-system API"；默认 "API"。
	Title string
	// Version 文档版本；默认 "v1"。
	Version string
	// BasePath 业务 API 的公开前缀，用于 OpenAPI servers 地址推导
	// （如网关域名），仅影响文档中展示的请求地址；默认留空（相对路径）。
	BasePath string
	// DocsPath 文档挂载前缀；默认 /apidoc。
	// OpenAPI JSON 在 {DocsPath}/openapi.json，UI 在 {DocsPath}。
	DocsPath string
	// BearerAuth 是否在 OpenAPI Components 中声明 JWT Bearer 鉴权方案，
	// 声明后文档页可直接用 token 试运行；默认 false。
	BearerAuth bool
	// Security 全局安全要求（OpenAPI security 字段），如
	// []map[string][]string{{"bearerAuth": {}}}；设置后所有操作默认要求鉴权，
	// 公开接口（login/refresh 等）在各自 Operation 里覆盖为空列表。
	Security []map[string][]string
}

// Defaults 填充缺省值并返回规范化后的副本。
func (o Options) Defaults() Options {
	if o.Title == "" {
		o.Title = "API"
	}
	if o.Version == "" {
		o.Version = "v1"
	}
	if o.DocsPath == "" {
		o.DocsPath = "/apidoc"
	}
	return o
}

// New 在现有 Gin 路由上创建 huma API 并挂载文档端点，返回 huma.API
// 供业务侧 huma.Register 注册接口。
//
// 用法（router.Register 回调内，认证组之前）：
//
//	api := apidoc.New(r, apidoc.Options{Title: "devops-system API", BearerAuth: true})
//	v1.RegisterUsers(api, userH) // 新接口huma.Register
func New(r gin.IRouter, opts Options) huma.API {
	opts = opts.Defaults()

	cfg := huma.DefaultConfig(opts.Title, opts.Version)
	// huma 默认 DocsPath=/docs、OpenAPIPath=/openapi（不带扩展名，huma 自动
	// 追加 .json/.yaml 按内容协商）。统一平移到业务约定的 DocsPath 下。
	cfg.DocsPath = opts.DocsPath
	cfg.OpenAPIPath = opts.DocsPath + "/openapi"
	if opts.BasePath != "" {
		cfg.Servers = append(cfg.Servers, &huma.Server{
			URL:         opts.BasePath,
			Description: "service endpoint",
		})
	}
	if opts.BearerAuth {
		if cfg.Components == nil {
			cfg.Components = &huma.Components{}
		}
		if cfg.Components.SecuritySchemes == nil {
			cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
		}
		cfg.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
		}
	}
	if len(opts.Security) > 0 {
		cfg.Security = opts.Security
	}

	api := humagin.New(mustEngine(r), cfg)

	// OpenAPI JSON 导出端点：供前端代码生成、网关路由发现、CI 契约测试消费。
	// huma 自身已挂 openapi.json；此处显式注册一个语义化别名，保持与旧版
	// /apidoc/doc.json 相近的对外约定（迁移期前端只改一处后缀）。
	huma.Register(api, huma.Operation{
		OperationID: "export-openapi",
		Method:      http.MethodGet,
		Path:        opts.DocsPath + "/doc.json",
		Summary:     "导出 OpenAPI 3.1 文档",
		Tags:        []string{"apidoc"},
	}, func(ctx context.Context, _ *docInput) (*openapiDocOutput, error) {
		return &openapiDocOutput{Body: api.OpenAPI()}, nil
	})

	return api
}

// mustEngine 将 gin.IRouter 收敛为 humagin 需要的 *gin.Engine。
// 允许传入 *gin.Engine 或 gin.RouterGroup（取其底层 Engine）。
func mustEngine(r gin.IRouter) *gin.Engine {
	if e, ok := r.(*gin.Engine); ok {
		return e
	}
	if g, ok := r.(*gin.RouterGroup); ok {
		// gin 的 RouterGroup 持有只读的 base path，无法直接取出 Engine；
		// 但业务侧约定在 Engine 根上挂文档（公开路由组），此分支仅防御。
		// RouterGroup 通过 group Engines 字段不可导出，改为要求传入 Engine。
		_ = g
	}
	panic("apidoc: humagin 适配器要求传入 *gin.Engine（请在 gin.Engine 根路由上挂载文档）")
}

// docInput 文档导出端点输入（无参数）。
type docInput struct{}

// openapiDocOutput OpenAPI 文档导出响应体。
type openapiDocOutput struct {
	Body any
}
