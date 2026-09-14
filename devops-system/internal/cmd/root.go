// Package cmd 定义 devops-system 的命令行入口。
//
// 配置管理：路径由 -f/--config-file 绑定，加载与读取封装在 devops-infra/config，
// cobra.OnInitialize 中完成加载，PersistentPreRunE 中完成组件初始化。
package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	infracfg "devops-infra/config"
	grpcinfra "devops-infra/grpc"
	httpinfra "devops-infra/http"
	"devops-infra/logging"

	"devops-id/pkg/idclient"
	client "devops-infra/database/mysql"
	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/config"
	"devops-system/internal/model"
	"devops-system/internal/router"
	"devops-system/internal/service"
	"devops-system/internal/store"
	"devops-system/internal/tenantdb"
)

var confFile string

var rootCmd = &cobra.Command{
	Use:   "devops-system",
	Short: "devOps 用户权限系统",

	// 所有子命令执行前都会先跑这里：初始化日志
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}
		if err := logging.Init(cfg.Log); err != nil {
			return err
		}

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
		return logging.Close()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(
		&confFile, "config-file", "f", "config/config.yaml", "the service config from file",
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

		// 装配链（方案 11.1）：db → TenantPlugin(fail-closed) → [模式B Resolver] → authz
		//   → store(Redis 会话) → idclient（发号）→ auth/service → httpx
		if err := client.Init(cfg.Mysql); err != nil {
			return err
		}
		if err := tenantdb.RegisterPlugin(client.Get()); err != nil {
			return fmt.Errorf("注册租户过滤插件失败: %w", err)
		}
		if cfg.Tenant.Mode == "schema" { // 模式B：按租户解析独立库连接池
			if err := tenantdb.InitResolver(cfg.Mysql, cfg.Tenant); err != nil {
				return err
			}
		}
		if err := authz.Init(client.Get()); err != nil {
			return err
		}
		if err := store.Init(cfg.Redis); err != nil {
			return err
		}
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

		authM := auth.New(cfg.JWT)
		authSvc := service.NewAuth(cfg, authM)
		userSvc := service.NewUser(authM)
		roleSvc := service.NewRole()
		menuSvc := service.NewMenu()
		deptSvc := service.NewDept()
		postSvc := service.NewPost()
		tenantSvc := service.NewTenant(cfg, authM)

		err = httpinfra.Run(cfg.Server, router.Register(authM, authSvc, userSvc,
			roleSvc, menuSvc, deptSvc, postSvc, tenantSvc))

		// 优雅退出顺序：先停 HTTP 排空（在途 Create 已取号），再关 resolver、authz、idclient、store、db
		tenantdb.CloseResolver()
		authz.Close()
		_ = grpcinfra.Close(conn)
		_ = store.Close()
		_ = client.Close()
		return err
	},
}
