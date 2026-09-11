// migrate 子命令：t_leaf_alloc 建表与 biz_tag 注册。
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	infracfg "devops-infra/config"

	"devops-id/internal/config"
	"devops-id/internal/db"
	"devops-id/internal/segment"
)

// bizTags 支持通过 --tags 额外注册的业务标识（逗号分隔）。
var bizTags string

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "建表（t_leaf_alloc）并注册 biz_tag 号段行",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg config.Config
		if err := infracfg.LoadInto(&cfg); err != nil {
			return err
		}

		if err := db.Init(cfg.Mysql); err != nil {
			return err
		}

		mgr := segment.New(db.Get(), segment.Config{
			Step:     cfg.IDGen.Step,
			LowWater: cfg.IDGen.LowWater,
		})

		if err := mgr.AutoMigrate(); err != nil {
			return fmt.Errorf("t_leaf_alloc 建表失败: %w", err)
		}

		// 默认注册 devops-system 六模块的号段行；--tags 可追加
		tags := []string{"user", "role", "menu", "dept", "post", "tenant"}
		for _, t := range strings.Split(bizTags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		if err := mgr.EnsureRegistered(tags, "devops-system 消费方"); err != nil {
			return err
		}

		fmt.Printf("migrate 完成：t_leaf_alloc 已注册 biz_tag: %s\n", strings.Join(tags, ", "))
		return nil
	},
}

func init() {
	migrateCmd.Flags().StringVar(&bizTags, "tags", "", "额外注册的 biz_tag，逗号分隔")
}
