// Package dbinit 启动期建表（纯 SQL 脚本，零记账）。
//
// 与 devops-system/internal/dbinit 的差异：
//   - **无种子数据**：C 端用户由用户自己注册，不预置任何账号，故只需 sql/schema；
//     无 sql/datas 目录，也就没有「种子仅对本次新建表执行」的逻辑。
//   - 同样不跨模块复用 system 的实现——它是 internal 包，不可被 devops-user 引用。
//
// 跳过判定：表已存在（information_schema 判存）则整个跳过。
//
// ⚠️ DDL 脚本变更不作用于已存在的表——存量库 schema 变更需手动 ALTER。
// 多实例并发首启靠 CREATE TABLE IF NOT EXISTS 兜底，不引入分布式锁。
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

//go:embed sql/schema
var scriptFS embed.FS

// tablePattern 文件名主干约定：表名。
var tablePattern = regexp.MustCompile(`^t_[a-z0-9_]+$`)

// createTableRE 建表语句开头：捕获组 1 为被创建的表名。
var createTableRE = regexp.MustCompile("(?i)^CREATE TABLE (?:IF NOT EXISTS )?`?([a-z0-9_]+)`?\\s*\\(")

// script 单个建表脚本（已拆分的语句序列）。
type script struct {
	Table string // 文件名主干（表名）
	Name  string // 完整文件名
	Stmts []string
}

// Run 对目标库执行建表脚本。
func Run(gdb *gorm.DB) error {
	return runFS(gdb, scriptFS)
}

// runFS 对给定文件系统执行建表脚本（独立给单测注入 MapFS）。
func runFS(gdb *gorm.DB, fsys fs.FS) error {
	ddls, err := loadScripts(fsys)
	if err != nil {
		return err
	}

	created := 0
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
		created++
		slog.Info("建表已执行", "table", s.Table, "file", s.Name)
	}

	slog.Info("dbinit 完成", "created", created, "skipped", skipped)
	return nil
}

// loadScripts 加载并校验 sql/schema 下全部 .sql（文件名主干 = 表名，按名排序）。
// 校验每份脚本恰含 1 条 CREATE TABLE 且表名与文件名主干一致。
func loadScripts(fsys fs.FS) ([]script, error) {
	paths, err := fs.Glob(fsys, "sql/schema/*.sql")
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
		out = append(out, script{Table: stem, Name: base, Stmts: stmts})
	}
	return out, nil
}

// tableExists 判表是否已存在：MySQL 查 information_schema；
// 其他方言（单测 sqlite）查 sqlite_master。
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

// splitStatements 按分号切分 SQL 文本为可执行语句序列：
//   - 单引号 '…'、双引号 "…"、反引号 `…` 字面量内的分号不算语句边界；
//   - MySQL 反斜杠转义（'\'）与相邻双写引号（'it”s'）均在字面量内原样保留；
//   - -- 行注释与 /* */ 块注释整体剥离（含被注释掉的分号）；
//   - 纯注释/空白段不产出语句。
//
// 方言边界：面向 MySQL（生产）与 sqlite（单测），不处理 PG 的 dollar-quoted 字符串。
func splitStatements(sql string) []string {
	var stmts []string
	var b strings.Builder
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			stmts = append(stmts, s)
		}
		b.Reset()
	}
	i, n := 0, len(sql)
	for i < n {
		switch c := sql[i]; {
		case c == '\'' || c == '"' || c == '`':
			i = copyQuoted(sql, i, &b)
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			i += 2
			for i+1 < n && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i += 2 // 跳过 "*/"；未闭合时 i 越界由外层条件兜住
		case c == ';':
			flush()
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	flush()
	return stmts
}

// copyQuoted 把 sql[i] 起的一个带引号字面量整体复制进 b，返回字面量
// 结束后的下标。引号未闭合时返回 len(sql)，语法错误交给数据库报错。
func copyQuoted(sql string, i int, b *strings.Builder) int {
	q := sql[i]
	b.WriteByte(q)
	i++
	for i < len(sql) {
		c := sql[i]
		b.WriteByte(c)
		i++
		switch {
		case c == '\\' && q != '`' && i < len(sql):
			b.WriteByte(sql[i]) // MySQL 反斜杠转义：带上下一个字符
			i++
		case c == q && i < len(sql) && sql[i] == q:
			b.WriteByte(q) // 相邻双写引号（''）是字面量内转义
			i++
		case c == q:
			return i
		}
	}
	return i
}
