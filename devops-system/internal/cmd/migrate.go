// migrate 子命令：AutoMigrate 建表 + 版本化 SQL 种子（internal/migration）。
//
// 与 server 启动链共用同一套迁移装配（autoMigrate + runDataMigrations），
// 手动 migrate 仅是运维兜底入口——正常情况下服务启动时自动完成。
//
// 单库模式：全部表和种子数据都写入配置的 MySQL 数据库。
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	infracfg "devops-infra/config"
	"devops-infra/database/mysql"
	"devops-system/internal/authz"
	"devops-system/internal/config"
	"devops-system/internal/db"
	"devops-system/internal/migration"
	"devops-system/internal/model"
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

		if err := autoMigrate(gdb); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
		if err := db.EnsureCompositeUniques(gdb); err != nil {
			return fmt.Errorf("创建唯一索引失败: %w", err)
		}
		if err := runDataMigrations(gdb); err != nil {
			return fmt.Errorf("数据迁移失败: %w", err)
		}
		if err := authz.Init(gdb); err != nil { // 建 casbin_rule 表并加载存量策略
			return err
		}
		defer authz.Close()
		if err := authz.SeedPolicies(1, 1); err != nil { // admin = 平台管理员 + 租户1超管
			return fmt.Errorf("casbin 策略种子失败: %w", err)
		}
		fmt.Println("migrate 完成：t_tenant + t_system_* 表 + casbin_rule + 版本化种子已就绪")
		return nil
	},
}

// autoMigrate 在单库模式下幂等地创建全部业务表。
func autoMigrate(gdb *gorm.DB) error {
	return gdb.AutoMigrate(
		&model.Tenant{},
		&model.User{},
		&model.Role{},
		&model.Menu{},
		&model.Dept{},
		&model.Post{},
		&model.UserRole{},
		&model.RoleMenu{},
		&model.LoginLog{},
		&model.OperationLog{},
	)
}

// runDataMigrations 版本化 SQL 数据迁移（server 启动与 migrate 子命令共用）。
//
// global scope（菜单母本 + 默认租户行）和 tenant scope（租户1业务数据）
// 都应用到同一个业务库。
// 执行过的版本由 t_system_migration 记账跳过（详见 migration 包）。
func runDataMigrations(gdb *gorm.DB) error {
	if err := migration.Run(gdb, migration.ScopeGlobal); err != nil {
		return fmt.Errorf("global scope 迁移失败: %w", err)
	}
	return migration.Run(gdb, migration.ScopeTenant)
}
