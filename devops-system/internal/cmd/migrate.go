// migrate 子命令：建表 SQL + 初始数据 SQL（internal/dbinit）。
//
// 与 server 启动链共用同一套初始化（dbinit.Run），手动 migrate 仅是运维
// 兜底入口——正常情况下服务启动时自动完成。已存在的表及其种子自动跳过。
//
// 单库模式：全部表和种子数据都写入配置的 MySQL 数据库。
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	infracfg "github.com/stanley760/devOps/devops-infra/config"
	"github.com/stanley760/devOps/devops-infra/database/mysql"

	"github.com/stanley760/devOps/devops-system/internal/authz"
	"github.com/stanley760/devOps/devops-system/internal/config"
	"github.com/stanley760/devOps/devops-system/internal/dbinit"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "建表 SQL + 初始数据 SQL（表存在即跳过，幂等，与 server 启动一致）",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}
		if err := mysql.Init(cfg.Mysql); err != nil {
			return err
		}
		gdb := mysql.Get()

		if err := dbinit.Run(gdb); err != nil {
			return fmt.Errorf("建表/种子失败: %w", err)
		}
		if err := authz.Init(gdb); err != nil { // 建 casbin_rule 表并加载存量策略
			return err
		}
		defer authz.Close()
		if err := authz.SeedPolicies(1, 1); err != nil { // admin = 平台管理员 + 租户1超管
			return fmt.Errorf("casbin 策略种子失败: %w", err)
		}
		fmt.Println("migrate 完成：建表 + 种子 SQL 就绪（已存在表自动跳过）；casbin_rule + 策略种子就绪")
		return nil
	},
}
