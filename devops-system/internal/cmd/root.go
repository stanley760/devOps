// Package cmd 定义 devops-system 的命令行入口。
//
// 配置管理：路径由 -f/--config-file 绑定，加载与读取封装在 devops-infra/config，
// cobra.OnInitialize 中完成加载，PersistentPreRunE 中完成组件初始化。
package cmd

import (
	"context"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	infracfg "devops-infra/config"
	grpcinfra "devops-infra/grpc"
	httpinfra "devops-infra/http"
	"devops-infra/logging"
	"devops-infra/nacos"
	"devops-infra/observability"
	securityjwt "devops-infra/security/jwt"
	securitysession "devops-infra/security/session"

	"devops-id/pkg/idclient"
	client "devops-infra/database/mysql"
	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/config"
	"devops-system/internal/db"
	"devops-system/internal/model"
	"devops-system/internal/router"
	"devops-system/internal/service"
	"devops-system/internal/store"
	"devops-system/internal/tenantdb"
)

var confFile string

var obsRuntime *observability.Runtime

var rootCmd = &cobra.Command{
	Use:   "devops-system",
	Short: "devOps 用户权限系统",

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
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		// 装配链（方案 11.1）：db → TenantPlugin(fail-closed) → [模式B Resolver] → 启动迁移
		//   （AutoMigrate + 版本化 SQL 种子，internal/migration）→ authz → store(Redis 会话)
		//   → 共享安全(RS256 JWT + session) → idclient（发号）→ auth/service → httpx
		if err := client.Init(cfg.Mysql); err != nil {
			return err
		}
		if err := obsRuntime.InstrumentGORM(client.Get(), cfg.Mysql.Database); err != nil {
			return err
		}
		if err := tenantdb.RegisterPlugin(client.Get()); err != nil {
			return fmt.Errorf("注册租户过滤插件失败: %w", err)
		}
		schemaMode := cfg.Tenant.Mode == "schema"
		if schemaMode { // 模式B：按租户解析独立库连接池
			if err := tenantdb.InitResolver(cfg.Mysql, cfg.Tenant); err != nil {
				return err
			}
		}

		// 启动期迁移（混合方案，详见 internal/migration 包文档）：
		// AutoMigrate 建表 → 复合唯一索引 → 版本化 SQL 种子（执行过即跳过）
		// → casbin。全部就绪后才注册 Nacos 接流量；失败 fail-closed 拒绝启动。
		if err := autoMigrate(client.Get(), schemaMode); err != nil {
			return fmt.Errorf("启动建表失败: %w", err)
		}
		if err := db.EnsureCompositeUniques(client.Get()); err != nil {
			return fmt.Errorf("创建唯一索引失败: %w", err)
		}
		if err := runDataMigrations(client.Get(), schemaMode); err != nil {
			return fmt.Errorf("启动数据迁移失败: %w", err)
		}
		if err := authz.Init(client.Get()); err != nil {
			return err
		}
		if err := authz.SeedPolicies(1, 1); err != nil { // 幂等（AddPolicyIfAbsent）
			return fmt.Errorf("casbin 策略种子失败: %w", err)
		}
		if err := store.Init(cfg.Redis); err != nil {
			return err
		}
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
		idc := idclient.New(conn, idclient.BufferConfig{
			Batch:       cfg.IDClient.Batch,
			LowWater:    cfg.IDClient.LowWater,
			CallTimeout: cfg.IDClient.CallTimeout,
		})
		model.SetIDGenerator(idc)

		authSvc := service.NewAuth(cfg, authM)
		userSvc := service.NewUser(authM)
		roleSvc := service.NewRole()
		menuSvc := service.NewMenu()
		deptSvc := service.NewDept()
		postSvc := service.NewPost()
		tenantSvc := service.NewTenant(cfg, authM)

		serviceName := cfg.Nacos.Registry.ServiceName
		if serviceName == "" {
			return fmt.Errorf("nacos.registry.service_name 未配置")
		}
		if err := nacos.Register(serviceName, cfg.Server.Port, cfg.Nacos.Registry.Meta); err != nil {
			return err
		}

		readiness := []httpinfra.ReadinessCheck{
			{Name: "mysql", Check: client.Default().Ping},
			{Name: "redis", Check: store.Ping},
			{Name: "devops-id", Check: idc.Ping},
		}
		err = httpinfra.RunWithOptions(cfg.Server, router.Register(cfg.APIDoc, authM, authSvc,
			userSvc, roleSvc, menuSvc, deptSvc, postSvc, tenantSvc), obsRuntime, readiness...)

		// 优雅退出顺序：先停 HTTP 排空（在途 Create 已取号），再关 resolver、authz、idclient、store、db
		_ = nacos.Deregister(serviceName, cfg.Server.Port)
		tenantdb.CloseResolver()
		authz.Close()
		_ = grpcinfra.Close(conn)
		_ = store.Close()
		_ = client.Close()
		return err
	},
}
