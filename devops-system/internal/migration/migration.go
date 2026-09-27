// Package migration 版本化 SQL 迁移执行器（混合迁移方案）。
//
// 职责边界：DDL 仍由 GORM AutoMigrate 负责（模型即代码），本包只管
// 「数据」的版本化 SQL——种子数据与后续数据变更写成有序 .sql 文件，
// go:embed 进二进制，服务启动时按序执行；执行过的版本记录在
// t_system_migration 版本表里直接跳过，天然满足「执行过不再执行」。
//
// scope（版本表 version 键带 scope 前缀，两 scope 可指向同一库）：
//   - ScopeGlobal  主库全局数据（菜单母本、t_tenant），两种模式都跑；
//   - ScopeTenant  租户业务数据（部门/岗位/角色/用户/授权）。
//     shared 模式目标即主库；模式B 目标为租户库（版本表随库记账）。
//
// 幂等与并发：
//   - 版本表 version 主键去重 + checksum 防已应用文件被篡改；
//   - MySQL 侧 GET_LOCK 串行化多实例启动，拿不到锁直接失败（fail-closed）；
//   - 每个 .sql 单事务执行（MySQL DDL 会隐式提交，数据文件不受影响）；
//   - 种子 INSERT 用 ON DUPLICATE KEY UPDATE 兼容「已跑过旧 Go 种子」
//     的存量库（版本表为空但数据已在）：首轮按 no-op 补记账，后续跳过。
package migration

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"sort"

	"gorm.io/gorm"
)

//go:embed sql/global/*.sql
var globalSQL embed.FS

//go:embed sql/tenant/*.sql
var tenantSQL embed.FS

// Scope 迁移作用域。
type Scope string

const (
	ScopeGlobal Scope = "global"
	ScopeTenant Scope = "tenant"
)

// versionTable 版本记录表（由执行器自建）。
const versionTable = "t_system_migration"

// namePattern 文件名约定：NNNN_name.sql，四位序号即版本号。
var namePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// migration 单个 SQL 迁移文件。
type migrationFile struct {
	Version  string // 四位序号，如 0001
	Name     string // 完整文件名
	Content  string // 原始内容（checksum 基准）
	Checksum string // sha256 hex
}

// Run 对目标库执行未应用的迁移：已应用版本校验 checksum 后跳过，
// 未应用版本按序号单事务执行并记账。scopes 至少传一个。
func Run(gdb *gorm.DB, scopes ...Scope) error {
	if len(scopes) == 0 {
		return fmt.Errorf("migration.Run 至少需要一个 scope")
	}
	for _, scope := range scopes {
		if err := runScopeFS(gdb, scope, scopeFS(scope)); err != nil {
			return err
		}
	}
	return nil
}

// scopeFS scope → 嵌入文件系统；未知 scope 报错（fail-closed）。
func scopeFS(scope Scope) fs.FS {
	switch scope {
	case ScopeGlobal:
		return globalSQL
	case ScopeTenant:
		return tenantSQL
	default:
		return failFS(fmt.Sprintf("未知迁移 scope: %s", scope))
	}
}

// failFS 哨兵文件系统：任何读取都返回给定错误（未知 scope 在 load 阶段 fail-closed）。
type errFS struct{ err error }

func (e errFS) Open(string) (fs.File, error) { return nil, e.err }

func failFS(msg string) fs.FS { return errFS{fmt.Errorf("%s", msg)} }

// runScopeFS 对目标库执行单个 scope 的迁移（ensure 版本表 → 加锁 → 补账）。
// 独立导出给单测注入 MapFS。
func runScopeFS(gdb *gorm.DB, scope Scope, fsys fs.FS) error {
	if err := ensureVersionTable(gdb); err != nil {
		return fmt.Errorf("创建版本表 %s 失败: %w", versionTable, err)
	}
	migs, err := load(fsys, scope)
	if err != nil {
		return fmt.Errorf("加载 %s 迁移文件失败: %w", scope, err)
	}
	return withLock(gdb, "devops-system:migration:"+string(scope), func() error {
		applied, err := appliedChecksums(gdb, scope)
		if err != nil {
			return err
		}
		for _, m := range migs {
			key := string(scope) + ":" + m.Version
			if sum, ok := applied[key]; ok {
				if sum != m.Checksum {
					return fmt.Errorf("迁移 %s 已应用但 checksum 不匹配（文件被改动？），拒绝启动", m.Name)
				}
				continue // 已应用：跳过
			}
			if err := apply(gdb, scope, m); err != nil {
				return fmt.Errorf("执行 %s 迁移 %s 失败: %w", scope, m.Name, err)
			}
			slog.Info("迁移已应用", "scope", scope, "version", m.Version, "file", m.Name)
		}
		return nil
	})
}

// ensureVersionTable 自建版本表（幂等；DDL 兼容 MySQL 与 sqlite 单测方言）。
func ensureVersionTable(gdb *gorm.DB) error {
	return gdb.Exec(`CREATE TABLE IF NOT EXISTS ` + versionTable + ` (
		version   VARCHAR(64)  NOT NULL PRIMARY KEY,
		name      VARCHAR(190) NOT NULL,
		checksum  CHAR(64)     NOT NULL,
		applied_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error
}

// appliedChecksums 读回该 scope 已应用版本的 checksum（键为 scope:version）。
func appliedChecksums(gdb *gorm.DB, scope Scope) (map[string]string, error) {
	var rows []struct {
		Version  string
		Checksum string
	}
	if err := gdb.Raw(
		"SELECT version, checksum FROM "+versionTable+" WHERE version LIKE ?",
		string(scope)+":%",
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取版本表失败: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Version] = r.Checksum
	}
	return out, nil
}

// apply 单事务执行一个迁移文件的全部语句并记账。
func apply(gdb *gorm.DB, scope Scope, m migrationFile) error {
	stmts := splitStatements(m.Content)
	if len(stmts) == 0 {
		return fmt.Errorf("文件无有效语句")
	}
	key := string(scope) + ":" + m.Version
	return gdb.Transaction(func(tx *gorm.DB) error {
		for _, s := range stmts {
			if err := tx.Exec(s).Error; err != nil {
				return err
			}
		}
		return tx.Exec(
			"INSERT INTO "+versionTable+" (version, name, checksum) VALUES (?, ?, ?)",
			key, m.Name, m.Checksum,
		).Error
	})
}

// load 读出 scope 目录全部 .sql，按序排序并校验命名与版本号唯一。
func load(fsys fs.FS, scope Scope) ([]migrationFile, error) {
	pattern := "sql/" + string(scope) + "/*.sql"
	paths, err := fs.Glob(fsys, pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	seen := make(map[string]bool, len(paths))
	migs := make([]migrationFile, 0, len(paths))
	for _, p := range paths {
		base := path.Base(p)
		mm := namePattern.FindStringSubmatch(base)
		if mm == nil {
			return nil, fmt.Errorf("文件名 %s 不符合 NNNN_name.sql 约定", base)
		}
		if seen[mm[1]] {
			return nil, fmt.Errorf("版本号 %s 重复：%s", mm[1], base)
		}
		seen[mm[1]] = true
		content, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		migs = append(migs, migrationFile{
			Version:  mm[1],
			Name:     base,
			Content:  string(content),
			Checksum: checksum(content),
		})
	}
	return migs, nil
}

// checksum sha256 hex（以原始文件字节为基准）。
func checksum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// withLock MySQL 咨询锁串行化多实例并发启动（等锁 30s，拿不到 fail-closed）。
// 非 MySQL 方言（单测 sqlite）无此函数，跳过加锁。
func withLock(gdb *gorm.DB, key string, fn func() error) error {
	if gdb.Dialector.Name() != "mysql" {
		return fn()
	}
	var got int
	if err := gdb.Raw("SELECT GET_LOCK(?, 30)", key).Scan(&got).Error; err != nil {
		return fmt.Errorf("获取迁移锁 %s 失败: %w", key, err)
	}
	if got != 1 {
		return fmt.Errorf("迁移锁 %s 被其他实例持有，等待 30s 超时", key)
	}
	defer func() { _ = gdb.Exec("SELECT RELEASE_LOCK(?)", key).Error }()
	return fn()
}
