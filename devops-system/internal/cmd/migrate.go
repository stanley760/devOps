// migrate 子命令：AutoMigrate 建表 + 版本化 SQL 种子（internal/migration）。
//
// 与 server 启动链共用同一套迁移装配（autoMigrate + runDataMigrations），
// 手动 migrate 仅是运维兜底入口——正常情况下服务启动时自动完成。
//
// 模式A（shared，默认）：主库建全部表 + 全量种子。
// 模式B（schema）：主库仅 t_tenant + 菜单母本（+ casbin）；tenant_default
// 库由 runDataMigrations provision；遍历 t_tenant 各租户库 AutoMigrate
// + 菜单 fan-out 同步（tenantdb.MigrateAllTenants）。
package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	infracfg "devops-infra/config"
	infratenant "devops-infra/tenant"
	"devops-infra/database/mysql"
	"devops-system/internal/authz"
	"devops-system/internal/config"
	"devops-system/internal/db"
	"devops-system/internal/migration"
	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "建表（t_tenant + t_system_*）并初始化种子数据（与 server 启动迁移一致）",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}
		if err := mysql.Init(cfg.Mysql); err != nil {
			return err
		}
		gdb := mysql.Get()

		schemaMode := cfg.Tenant.Mode == "schema"
		if schemaMode {
			if err := tenantdb.InitResolver(cfg.Mysql, cfg.Tenant); err != nil {
				return err
			}
			defer tenantdb.CloseResolver()
		}

		if err := autoMigrate(gdb, schemaMode); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
		if err := db.EnsureCompositeUniques(gdb); err != nil {
			return fmt.Errorf("创建唯一索引失败: %w", err)
		}
		if err := runDataMigrations(gdb, schemaMode); err != nil {
			return fmt.Errorf("数据迁移失败: %w", err)
		}
		if err := authz.Init(gdb); err != nil { // 建 casbin_rule 表并加载存量策略
			return err
		}
		defer authz.Close()
		if schemaMode { // 各租户库迁移 + 菜单 fan-out（幂等）
			if err := tenantdb.MigrateAllTenants(); err != nil {
				return err
			}
		}
		if err := authz.SeedPolicies(1, 1); err != nil { // admin = 平台管理员 + 租户1超管
			return fmt.Errorf("casbin 策略种子失败: %w", err)
		}
		fmt.Println("migrate 完成：t_tenant + t_system_* 表 + casbin_rule + 版本化种子已就绪")
		return nil
	},
}

// autoMigrate 建表（幂等）。模式A 主库建全部表；模式B 主库仅全局表
// （t_tenant + 菜单母本），租户表由 provision 建在各租户库——
// 主库不建租户表使「无租户 ctx 误路由主库」表现为表不存在错误（fail-closed）。
func autoMigrate(gdb *gorm.DB, schemaMode bool) error {
	if schemaMode {
		return gdb.AutoMigrate(&model.Tenant{}, &model.Menu{})
	}
	return gdb.AutoMigrate(
		&model.Tenant{},
		&model.User{},
		&model.Role{},
		&model.Menu{},
		&model.Dept{},
		&model.Post{},
		&model.UserRole{},
		&model.RoleMenu{},
	)
}

// runDataMigrations 版本化 SQL 数据迁移（server 启动与 migrate 子命令共用）。
//
// global scope（主库）：菜单母本 + 默认租户行，两种模式都执行；
// tenant scope（租户1业务数据）：shared 模式目标即主库；模式B 落
// tenant_default 库——先 provision 建库建表，再在该库的版本表里记账。
// 执行过的版本由 t_system_migration 记账跳过（详见 migration 包）。
func runDataMigrations(gdb *gorm.DB, schemaMode bool) error {
	if err := migration.Run(gdb, migration.ScopeGlobal); err != nil {
		return fmt.Errorf("global scope 迁移失败: %w", err)
	}
	if !schemaMode {
		return migration.Run(gdb, migration.ScopeTenant)
	}
	// 模式B：global scope 已写入 t_tenant 行，此处为默认租户建库建表
	if err := tenantdb.ProvisionSchema(context.Background(), "default"); err != nil {
		return err
	}
	tctx := infratenant.With(context.Background(), 1, "default")
	return migration.Run(tenantdb.DB(tctx), migration.ScopeTenant)
}
