// Package cmd 定义 devops-user 的命令行入口。
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
	securityjwt "github.com/stanley760/devops/devops-infra/security/jwt"
	securitysession "github.com/stanley760/devops/devops-infra/security/session"

	client "github.com/stanley760/devops/devops-infra/database/mysql"

	"github.com/stanley760/devops/devops-id/pkg/idclient"
	"github.com/stanley760/devops/devops-user/internal/auth"
	"github.com/stanley760/devops/devops-user/internal/config"
	"github.com/stanley760/devops/devops-user/internal/dbinit"
	"github.com/stanley760/devops/devops-user/internal/loginlock"
	"github.com/stanley760/devops/devops-user/internal/middleware"
	"github.com/stanley760/devops/devops-user/internal/model"
	cendoauth2 "github.com/stanley760/devops/devops-user/internal/oauth2"
	"github.com/stanley760/devops/devops-user/internal/refreshtoken"
	"github.com/stanley760/devops/devops-user/internal/router"
	"github.com/stanley760/devops/devops-user/internal/service"
	"github.com/stanley760/devops/devops-user/internal/store"
)

var confFile string

var obsRuntime *observability.Runtime

// refreshKeyPrefix refresh token 与登录限流计数的 Redis 键前缀。
// 与 devops-infra/security/session 的 `t{tid}:auth:*` 命名空间保持同一风格；
// C 端单租户故固定 t1（与 auth.DefaultTenantID 一致）。
const refreshKeyPrefix = "t1"

var rootCmd = &cobra.Command{
	Use:   "devops-user",
	Short: "devops C端用户服务",

	// 所有子命令执行前都会先跑这里：加载配置、初始化日志与可观测。
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
			"service", "devops-user",
			"config", infracfg.Files(),
			"env", infracfg.Env(),
			"logLevel", cfg.Log.Level,
		)
		return nil
	},

	// 所有子命令执行后统一收尾，保证日志不丢。
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

	// cobra.OnInitialize 会在每个命令的 Run 之前执行，此处完成配置加载。
	cobra.OnInitialize(func() {
		if err := infracfg.Init(confFile); err != nil {
			panic(err)
		}
	})
	rootCmd.AddCommand(serverCmd)
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

		// 装配链：db → dbinit(建表) → idclient(发号) → store(Redis 会话)
		//   → 共享安全(RS256 JWT + session) → 业务中间件套件 → service → router。
		if err := client.Init(cfg.Mysql); err != nil {
			return err
		}
		addCleanup("mysql", client.Close)
		if obsRuntime != nil {
			if err := obsRuntime.InstrumentGORM(client.Get(), cfg.Mysql.Database); err != nil {
				return err
			}
		}

		// 建表（无种子数据）：按表存在性跳过，表已存在不再执行。
		// 放在 Nacos 注册之前——表没建好就不该接流量。
		if err := dbinit.Run(client.Get()); err != nil {
			return fmt.Errorf("启动建表失败: %w", err)
		}

		// 发号服务（主键由 devops-id 分配，非自增）。装在 dbinit 之后：
		// 建表不需要 ID（无种子数据），但运行期插入需要。
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
		model.ConfigureModel(idc)

		if err := store.Init(cfg.Redis); err != nil {
			return err
		}
		addCleanup("redis", store.Close)
		if obsRuntime != nil {
			if err := obsRuntime.InstrumentRedis(store.Get()); err != nil {
				return err
			}
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

		// refresh token 存储（不透明随机串，非 JWT）与登录限流器，
		// 均以 t1 前缀复用同一 Redis。
		refreshStore := refreshtoken.NewStore(store.Get(), refreshKeyPrefix, cfg.JWT.RefreshExpire)
		authM := auth.New(cfg.JWT, tokenMgr, sessionStore, refreshStore)

		// 业务中间件套件（C 端无 RBAC/租户，仅注入信封文案）。
		kit := middleware.NewKit(authM, middleware.Deps{})

		// 业务用例
		limiter := loginlock.New(store.Get(), refreshKeyPrefix, loginlock.Config{
			Enabled:      cfg.LoginLock.Enabled,
			MaxIPFails:   cfg.LoginLock.MaxIPFails,
			IPWindow:     cfg.LoginLock.IPWindow,
			MaxUserFails: cfg.LoginLock.MaxUserFails,
			UserWindow:   cfg.LoginLock.UserWindow,
			LockTTL:      cfg.LoginLock.LockTTL,
		})
		authSvc := service.NewAuth(authM, limiter)
		profileSvc := service.NewProfile(authM, limiter)

		// 三方登录（GitHub / Google / 微信）。
		// 凭据未配齐的提供方仍会注册但 Enabled()=false，前端据此隐藏入口。
		oauthProviders := map[string]cendoauth2.Provider{}
		autoReg := map[string]bool{}
		prefixes := map[string]string{}
		for name, pc := range map[string]config.ProviderConfig{
			"github": cfg.OAuth.Github,
			"google": cfg.OAuth.Google,
			"wechat": cfg.OAuth.Wechat,
		} {
			oc := cendoauth2.Config{
				ClientID:       pc.ClientID,
				ClientSecret:   pc.ClientSecret,
				RedirectURL:    pc.RedirectURL,
				Scopes:         pc.Scopes,
				AutoRegister:   pc.AutoRegister,
				NicknamePrefix: pc.NicknamePrefix,
			}
			switch name {
			case "github":
				oauthProviders[name] = cendoauth2.NewGitHubProvider(oc)
			case "google":
				oauthProviders[name] = cendoauth2.NewGoogleProvider(oc)
			case "wechat":
				oauthProviders[name] = cendoauth2.NewWeChatProvider(oc)
			}
			autoReg[name] = pc.AutoRegister
			prefixes[name] = pc.NicknamePrefix
		}
		stateStore := cendoauth2.NewRedisStateStore(store.Get(), refreshKeyPrefix, cfg.OAuth.StateTTL)
		oauthSvc := service.NewOAuth(service.OAuthDeps{
			AuthM:             authM,
			Providers:         oauthProviders,
			States:            stateStore,
			StateTTL:          cfg.OAuth.StateTTL,
			AutoRegister:      autoReg,
			NicknamePrefix:    prefixes,
			RedirectWhitelist: cfg.OAuth.RedirectWhitelist,
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
		}
		register := router.NewRegister(cfg.APIDoc, authM, router.Modules{
			Auth:    router.NewAuthRoutes(authSvc, kit),
			Profile: router.NewProfileRoutes(profileSvc, kit),
			OAuth:   router.NewOAuthRoutes(oauthSvc, kit),
		})

		// 流量入口收口（第三层）：要求请求携带可独立校验通过的 Bearer JWT。
		// 放行名单：健康检查/指标（编排探针与 Prometheus 直连）、
		// 匿名端点（登录/注册/jwks——此时调用方尚无 token）、
		// 文档挂载点（仅 dev 开启）。
		guard := &httpinfra.GuardHooks{Guards: []gin.HandlerFunc{
			gateway.EnforceFromConfig(cfg.Gateway, authM,
				[]string{
					"/healthz", "/readyz", "/metrics",
					router.AuthLoginPath, router.AuthRegisterPath,
					router.AuthRefreshPath, router.JWKSPath,
				},
				[]string{cfg.APIDoc.BasePath},
			),
		}}
		return httpinfra.RunWithOptions(cfg.Server, register, obsRuntime, guard, readiness...)
	},
}
