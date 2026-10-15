// Package cmd 定义 devops-system 的命令行入口。
//
// 配置管理：路径由 -f/--config-file 绑定，加载与读取封装在 devops-infra/config，
// cobra.OnInitialize 中完成加载，PersistentPreRunE 中完成组件初始化。
package cmd

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"

	infracfg "github.com/stanley760/devops/devops-infra/config"
	grpcinfra "github.com/stanley760/devops/devops-infra/grpc"
	httpinfra "github.com/stanley760/devops/devops-infra/http"
	"github.com/stanley760/devops/devops-infra/logging"
	"github.com/stanley760/devops/devops-infra/nacos"
	"github.com/stanley760/devops/devops-infra/observability"
	"github.com/stanley760/devops/devops-infra/security/gateway"
	"github.com/stanley760/devops/devops-infra/security/humakit"
	securityjwt "github.com/stanley760/devops/devops-infra/security/jwt"
	securitysession "github.com/stanley760/devops/devops-infra/security/session"

	client "github.com/stanley760/devops/devops-infra/database/mysql"

	"github.com/stanley760/devops/devops-id/pkg/idclient"
	"github.com/stanley760/devops/devops-system/internal/app"
	sysaudit "github.com/stanley760/devops/devops-system/internal/audit"
	"github.com/stanley760/devops/devops-system/internal/auth"
	"github.com/stanley760/devops/devops-system/internal/authz"
	"github.com/stanley760/devops/devops-system/internal/config"
	"github.com/stanley760/devops/devops-system/internal/dbinit"
	"github.com/stanley760/devops/devops-system/internal/middleware"
	"github.com/stanley760/devops/devops-system/internal/model"
	"github.com/stanley760/devops/devops-system/internal/router"
	"github.com/stanley760/devops/devops-system/internal/store"
	"github.com/stanley760/devops/devops-system/internal/tenantdb"
)

var confFile string

var obsRuntime *observability.Runtime

var rootCmd = &cobra.Command{
	Use:   "devops-system",
	Short: "devops 用户权限系统",

	// 所有子命令执行前都会先跑这里：初始化日志
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}
		if err := loadNacosConfig(&cfg); err != nil {
			return err
		}
		if err := logging.Init(cfg.Log); err != nil {
			return err
		}
		obs, err := observability.Init(cmd.Context(), observability.Config{
			ServiceName:    cfg.Nacos.Registry.ServiceName,
			Env:            infracfg.Env(),
			OTLPEndpoint:   cfg.Observability.OTLPEndpoint,
			SamplerRatio:   cfg.Observability.SamplerRatio,
			MetricsEnabled: cfg.Observability.MetricsEnabled,
		})
		if err != nil {
			return err
		}
		obsRuntime = obs

		slog.Debug("初始化完成",
			"service", "devops-system",
			"config", infracfg.Files(),
			"env", infracfg.Env(),
			"logLevel", cfg.Log.Level,
		)
		return nil
	},

	// 所有子命令执行后统一收尾，保证日志不丢
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		_ = nacos.Close()
		if obsRuntime != nil {
			_ = obsRuntime.ShutdownWithTimeout(context.Background(), 5*time.Second)
			obsRuntime = nil
		}
		return logging.Close()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// loadNacosConfig 拉取远程 Nacos 配置并合并，远程优先：
// 命中远程 dataID 即以远程为准；仅当远程未命中 profile dataID
// （<service>-<env>.yaml）时，才合并本地同名环境文件兜底。
// bootstrap.yaml 始终作为公共基线。
func loadNacosConfig(cfg *config.Config) error {
	if !cfg.Nacos.Enabled {
		return nil
	}
	if err := nacos.Init(cfg.Nacos); err != nil {
		return err
	}
	dataIDs := infracfg.RemoteDataIDs(cfg.Nacos.ConfigCenter.DataIDs)
	profileDataID := infracfg.ProfileDataID()
	loaded := false
	profileLoaded := false
	for _, dataID := range dataIDs {
		content, err := nacos.Fetch(dataID)
		if err != nil {
			if cfg.Nacos.ConfigCenter.Required {
				return err
			}
			continue
		}
		if content == "" {
			continue
		}
		if err := infracfg.MergeRemoteYAML(dataID, content); err != nil {
			return err
		}
		loaded = true
		if dataID == profileDataID {
			profileLoaded = true
		}
	}
	if cfg.Nacos.ConfigCenter.Required && !loaded {
		return fmt.Errorf("nacos 配置中心未返回配置")
	}
	if !profileLoaded {
		// 远程未命中 profile 配置：合并本地 <service>-<env>.yaml 兜底
		if err := infracfg.MergeEnvConfig(); err != nil {
			return err
		}
	}
	return infracfg.LoadInto(cfg)
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&confFile, "config-file", "f", "config/bootstrap.yaml", "the bootstrap config file",
	)
}

// Execute 执行根命令。version 用于 --version 输出。
func Execute(version string) {
	rootCmd.Version = version

	// cobra.OnInitialize 会在每个命令的 Run 之前执行，此处完成配置加载
	cobra.OnInitialize(func() {
		if err := infracfg.Init(confFile); err != nil {
			panic(err)
		}
	})
	rootCmd.AddCommand(serverCmd, migrateCmd)
	if err := rootCmd.Execute(); err != nil {
		cobra.CheckErr(err)
	}
}

// serverCmd 启动 HTTP 服务。
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "启动 HTTP 服务",
	RunE: func(cmd *cobra.Command, args []string) (runErr error) {
		type cleanup struct {
			name string
			run  func() error
		}
		var cleanups []cleanup
		addCleanup := func(name string, run func() error) {
			cleanups = append(cleanups, cleanup{name: name, run: run})
		}
		defer func() {
			for i := len(cleanups) - 1; i >= 0; i-- {
				if err := cleanups[i].run(); err != nil {
					runErr = errors.Join(runErr, fmt.Errorf("清理 %s 失败: %w", cleanups[i].name, err))
				}
			}
		}()

		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		// 装配链：db → TenantPlugin(fail-closed) → 启动初始化
		//   （建表 SQL + 种子 SQL，internal/dbinit）→ authz → store(Redis 会话)
		//   → 共享安全(RS256 JWT + session) → idclient（发号）→ auth/service → httpx
		if err := client.Init(cfg.Mysql); err != nil {
			return err
		}
		addCleanup("mysql", client.Close)
		if err := obsRuntime.InstrumentGORM(client.Get(), cfg.Mysql.Database); err != nil {
			return err
		}
		if err := tenantdb.RegisterPlugin(client.Get()); err != nil {
			return fmt.Errorf("注册租户过滤插件失败: %w", err)
		}
		// 启动期初始化（详见 internal/dbinit 包文档）：建表 SQL + 种子 SQL
		// 按表存在性跳过（表已存在不再执行）→ casbin。全部就绪后才注册
		// Nacos 接流量；失败 fail-closed 拒绝启动。
		if err := dbinit.Run(client.Get()); err != nil {
			return fmt.Errorf("启动建表/种子失败: %w", err)
		}
		if err := authz.Init(client.Get()); err != nil {
			return err
		}
		addCleanup("authz", func() error {
			authz.Close()
			return nil
		})
		if err := authz.SeedPolicies(1, 1); err != nil { // 幂等（AddPolicyIfAbsent）
			return fmt.Errorf("casbin 策略种子失败: %w", err)
		}
		if err := store.Init(cfg.Redis); err != nil {
			return err
		}
		addCleanup("redis", store.Close)
		if err := obsRuntime.InstrumentRedis(store.Get()); err != nil {
			return err
		}

		// 共享安全装配：配置私钥 → 自验签 KeySet（单签发方；轮换期可扩展多公钥/JWKS）
		pub, err := securityjwt.PublicKeyFromPrivatePEM([]byte(cfg.JWT.SigningKey))
		if err != nil {
			return fmt.Errorf("解析 JWT 私钥失败（jwt.signing_key 须为 RSA 私钥 PEM）: %w", err)
		}
		keySet, err := securityjwt.NewStaticKeySet(map[string]*rsa.PublicKey{
			cfg.JWT.EffectiveKeyID(): pub,
		})
		if err != nil {
			return fmt.Errorf("构建 JWT KeySet 失败: %w", err)
		}
		tokenMgr, err := securityjwt.NewManager(cfg.JWT.SharedJWTConfig(keySet))
		if err != nil {
			return fmt.Errorf("初始化共享 JWT 管理器失败: %w", err)
		}
		sessionStore := securitysession.NewRedisClientStore(store.Get(), cfg.JWT.RefreshExpire)
		authM := auth.New(cfg.JWT, tokenMgr, sessionStore)
		conn, err := grpcinfra.Dial(cmd.Context(), cfg.IDClient.Endpoint, cfg.IDClient.DialTimeout)
		if err != nil {
			return err
		}
		addCleanup("devops-id 连接", func() error { return grpcinfra.Close(conn) })
		idc := idclient.New(conn, idclient.BufferConfig{
			Batch:       cfg.IDClient.Batch,
			LowWater:    cfg.IDClient.LowWater,
			CallTimeout: cfg.IDClient.CallTimeout,
		})
		model.SetIDGenerator(idc)

		// 业务中间件套件（D7：业务依赖经 Deps 注入，nil 项由 NewKit 补生产默认绑定）。
		kit := middleware.NewKit(authM, middleware.Deps{})

		// 操作审计装配：Sink 落本租户 t_system_operation_log，采集范围由
		// router 包的路由白名单决定（只审计写操作，见 audit_modules.go）。
		// 审计是旁路：写入失败仅 slog.Warn，不影响业务响应。
		kit.EnableAudit(humakit.AuditDeps{
			Sink:    sysaudit.NewSink(),
			Resolve: router.AuditModuleResolver(),
		})

		serviceName := cfg.Nacos.Registry.ServiceName
		if serviceName == "" {
			return fmt.Errorf("nacos.registry.service_name 未配置")
		}
		if err := nacos.Register(serviceName, cfg.Server.Port, cfg.Nacos.Registry.Meta); err != nil {
			return err
		}
		addCleanup("nacos 服务注册", func() error {
			return nacos.Deregister(serviceName, cfg.Server.Port)
		})

		readiness := []httpinfra.ReadinessCheck{
			{Name: "mysql", Check: client.Default().Ping},
			{Name: "redis", Check: store.Ping},
			{Name: "devops-id", Check: idc.Ping},
		}
		register := app.InitializeHTTP(cfg, kit, authM)

		// 流量入口收口（第三层）：拒绝未经网关的直连请求。
		// 网关侧以 request-header-control-update 覆盖注入 X-Gateway-Token
		// （见 deploy/higress/ingresses/devops-system.yaml），客户端伪造无效。
		// 运维端点不经网关，列入放行名单，否则健康检查与 Prometheus 抓取会被拦。
		guard := &httpinfra.GuardHooks{Guards: []gin.HandlerFunc{
			gateway.EnforceFromConfig(cfg.Gateway,
				// 健康检查与指标：容器编排与 Prometheus 直连抓取。
				[]string{"/healthz", "/readyz", "/metrics"},
				// 接口文档挂载点：仅 dev/test 开启（apidoc.enabled 控制）。
				[]string{cfg.APIDoc.BasePath},
			),
		}}
		return httpinfra.RunWithOptions(cfg.Server, register, obsRuntime, guard, readiness...)
	},
}
