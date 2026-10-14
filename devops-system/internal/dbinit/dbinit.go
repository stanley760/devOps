// Package dbinit 启动期建表与初始数据初始化（纯 SQL 脚本，零记账）。
//
// 职责边界：建表与初始数据都写成 .sql 脚本嵌入二进制，服务启动完成前
// （server 装配链 / migrate 子命令）执行一次。
//
// 脚本布局（文件名主干 = 表名，须匹配 ^t_[a-z0-9_]+$）：
//   - sql/schema/<table>.sql   每表一个 CREATE TABLE IF NOT EXISTS；
//   - sql/datas/<table>.sql  每表的初始数据（无种子的表不放文件）。
//
// 跳过判定：
//   - 建表：表已存在（information_schema 判存）整个跳过；
//   - 种子：仅当对应表刚被本次创建才执行（表已存在 = 数据已初始化）。
//
// 注意：DDL 脚本变更不作用于已存在的表——存量库 schema 变更需手动 ALTER。
// 多实例并发首启靠 CREATE TABLE IF NOT EXISTS 与种子 ON DUPLICATE KEY
// UPDATE id = id 兜底，不引入分布式锁。
package dbinit

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed sql/schema sql/datas
var scriptFS embed.FS

// tablePattern 文件名主干约定：表名。
var tablePattern = regexp.MustCompile(`^t_[a-z0-9_]+$`)

// createTableRE 建表语句开头：捕获组 1 为被创建的表名。
var createTableRE = regexp.MustCompile("(?i)^CREATE TABLE (?:IF NOT EXISTS )?`?([a-z0-9_]+)`?\\s*\\(")

// script 单个 SQL 脚本（已拆分的语句序列）。
type script struct {
	Table string // 文件名主干（表名）
	Name  string // 完整文件名
	Stmts []string
}

// Run 对目标库执行建表与初始数据脚本（详见包文档）。
func Run(gdb *gorm.DB) error {
	return runFS(gdb, scriptFS)
}

// runFS 对给定文件系统执行建表与种子脚本（独立给单测注入 MapFS）。
func runFS(gdb *gorm.DB, fsys fs.FS) error {
	ddls, err := loadScripts(fsys, "sql/schema", true)
	if err != nil {
		return fmt.Errorf("加载建表脚本失败: %w", err)
	}
	datas, err := loadScripts(fsys, "sql/datas", false)
	if err != nil {
		return fmt.Errorf("加载种子脚本失败: %w", err)
	}
	ddlTables := make(map[string]bool, len(ddls))
	for _, s := range ddls {
		ddlTables[s.Table] = true
	}
	for _, s := range datas {
		if !ddlTables[s.Table] {
			return fmt.Errorf("种子脚本 %s 没有对应的建表脚本 sql/schema/%s", s.Name, s.Name)
		}
	}

	created := make(map[string]bool)
	skipped := 0
	for _, s := range ddls {
		exists, err := tableExists(gdb, s.Table)
		if err != nil {
			return err
		}
		if exists {
			skipped++
			slog.Info("表已存在，跳过建表", "table", s.Table, "file", s.Name)
			continue
		}
		for _, stmt := range s.Stmts {
			if err := gdb.Exec(stmt).Error; err != nil {
				return fmt.Errorf("执行建表脚本 %s 失败: %w", s.Name, err)
			}
		}
		created[s.Table] = true
		slog.Info("建表已执行", "table", s.Table, "file", s.Name)
	}

	seeded := 0
	for _, s := range datas {
		if !created[s.Table] {
			slog.Info("种子跳过（表非本次创建）", "table", s.Table, "file", s.Name)
			continue
		}
		if err := gdb.Transaction(func(tx *gorm.DB) error {
			for _, stmt := range s.Stmts {
				if err := tx.Exec(stmt).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("执行种子脚本 %s 失败: %w", s.Name, err)
		}
		seeded++
		slog.Info("种子数据已执行", "table", s.Table, "file", s.Name)
	}

	slog.Info("dbinit 完成", "created", len(created), "seeded", seeded, "skipped", skipped)
	return nil
}

// loadScripts 加载并校验 dir 下全部 .sql（文件名主干 = 表名，按名排序）。
// isDDL 时校验恰含 1 条 CREATE TABLE 且表名与文件名主干一致。
func loadScripts(fsys fs.FS, dir string, isDDL bool) ([]script, error) {
	paths, err := fs.Glob(fsys, dir+"/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]script, 0, len(paths))
	for _, p := range paths {
		base := path.Base(p)
		stem := strings.TrimSuffix(base, ".sql")
		if !tablePattern.MatchString(stem) {
			return nil, fmt.Errorf("文件名 %s 不合规（须为 <表名>.sql，表名匹配 ^t_[a-z0-9_]+$）", base)
		}
		content, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		stmts := splitStatements(string(content))
		if isDDL {
			if len(stmts) != 1 {
				return nil, fmt.Errorf("建表脚本 %s 须恰含 1 条 CREATE TABLE 语句，实际 %d 条", base, len(stmts))
			}
			m := createTableRE.FindStringSubmatch(stmts[0])
			if m == nil {
				return nil, fmt.Errorf("建表脚本 %s 的语句不是 CREATE TABLE", base)
			}
			if m[1] != stem {
				return nil, fmt.Errorf("建表脚本 %s 创建的表 %s 与文件名主干不一致", base, m[1])
			}
		} else if len(stmts) == 0 {
			return nil, fmt.Errorf("种子脚本 %s 无有效语句", base)
		}
		out = append(out, script{Table: stem, Name: base, Stmts: stmts})
	}
	return out, nil
}

// tableExists 判表是否已存在：MySQL 查 information_schema（MySQL/MariaDB
// 通用）；其他方言（单测 sqlite）查 sqlite_master。
func tableExists(gdb *gorm.DB, table string) (bool, error) {
	var cnt int64
	var err error
	if gdb.Dialector.Name() == "mysql" {
		err = gdb.Raw(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			table,
		).Scan(&cnt).Error
	} else {
		err = gdb.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			table,
		).Scan(&cnt).Error
	}
	if err != nil {
		return false, fmt.Errorf("查询表 %s 是否存在失败: %w", table, err)
	}
	return cnt > 0, nil
}
