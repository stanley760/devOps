package dbinit

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testDB 每用例独立内存库（sqlite 方言：tableExists 走 sqlite_master 分支）。
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// mapFS 构造脚本文件系统（相对 sql/ 的路径 → 内容）。
// 单测用 sqlite 兼容 SQL，不执行生产 MySQL 脚本（后者只做命名/结构校验）。
func mapFS(files map[string]string) fstest.MapFS {
	fs := fstest.MapFS{}
	for name, content := range files {
		fs["sql/"+name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fs
}

func TestRunCreatesTablesAndSeeds(t *testing.T) {
	db := testDB(t)
	fs := mapFS(map[string]string{
		"ddl/t_demo.sql":  "CREATE TABLE IF NOT EXISTS t_demo (id INTEGER PRIMARY KEY, v TEXT);",
		"data/t_demo.sql": "INSERT INTO t_demo (id, v) VALUES (1, 'a');",
	})
	if err := runFS(db, fs); err != nil {
		t.Fatalf("first run: %v", err)
	}
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM t_demo").Scan(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("rows = %d, want 1", cnt)
	}
}

func TestRunSecondRunSkipsEverything(t *testing.T) {
	db := testDB(t)
	fs := mapFS(map[string]string{
		"ddl/t_demo.sql":  "CREATE TABLE IF NOT EXISTS t_demo (id INTEGER PRIMARY KEY, v TEXT);",
		"data/t_demo.sql": "INSERT INTO t_demo (id, v) VALUES (1, 'a');",
	})
	if err := runFS(db, fs); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// 第二次执行：表已存在 → 建表与种子全部跳过，数据不重复
	if err := runFS(db, fs); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM t_demo").Scan(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("re-run rows = %d, want 1（表已存在应跳过种子）", cnt)
	}
}

func TestRunDataSkippedWhenTablePreExists(t *testing.T) {
	db := testDB(t)
	// t_a 先于 dbinit 存在（存量表）；t_b 由本次创建
	if err := db.Exec("CREATE TABLE t_a (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("pre-create t_a: %v", err)
	}
	fs := mapFS(map[string]string{
		"ddl/t_a.sql":  "CREATE TABLE IF NOT EXISTS t_a (id INTEGER PRIMARY KEY);",
		"data/t_a.sql": "INSERT INTO t_a (id) VALUES (1);",
		"ddl/t_b.sql":  "CREATE TABLE IF NOT EXISTS t_b (id INTEGER PRIMARY KEY);",
		"data/t_b.sql": "INSERT INTO t_b (id) VALUES (1);",
	})
	if err := runFS(db, fs); err != nil {
		t.Fatalf("run: %v", err)
	}
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM t_a").Scan(&cnt).Error; err != nil {
		t.Fatalf("count t_a: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("t_a rows = %d, want 0（表非本次创建，种子应跳过）", cnt)
	}
	if err := db.Raw("SELECT COUNT(*) FROM t_b").Scan(&cnt).Error; err != nil {
		t.Fatalf("count t_b: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("t_b rows = %d, want 1（表本次创建，种子应执行）", cnt)
	}
}

func TestRunDDLFailureFailsClosed(t *testing.T) {
	// 单条建表语句语法错误（括号未闭合）：执行期 fail-closed
	fs := mapFS(map[string]string{
		"ddl/t_demo.sql": "CREATE TABLE IF NOT EXISTS t_demo (id INTEGER PRIMARY KEY, v TEXT",
	})
	if err := runFS(testDB(t), fs); err == nil {
		t.Fatal("坏建表语句执行失败应报错")
	}
}

func TestRunRejectsMultiStmtDDL(t *testing.T) {
	fs := mapFS(map[string]string{
		"ddl/t_demo.sql": "CREATE TABLE IF NOT EXISTS t_demo (id INTEGER PRIMARY KEY); INSERT INTO t_demo (id) VALUES (1);",
	})
	err := runFS(testDB(t), fs)
	if err == nil || !strings.Contains(err.Error(), "恰含 1 条") {
		t.Fatalf("建表脚本非单条语句应报错, got: %v", err)
	}
}

func TestRunRejectsStemContentMismatch(t *testing.T) {
	fs := mapFS(map[string]string{
		"ddl/t_x.sql": "CREATE TABLE IF NOT EXISTS t_y (id INTEGER PRIMARY KEY);",
	})
	err := runFS(testDB(t), fs)
	if err == nil || !strings.Contains(err.Error(), "与文件名主干不一致") {
		t.Fatalf("表名与文件名不一致应报错, got: %v", err)
	}
}

func TestRunRejectsDataWithoutDdl(t *testing.T) {
	fs := mapFS(map[string]string{
		"ddl/t_a.sql":  "CREATE TABLE IF NOT EXISTS t_a (id INTEGER PRIMARY KEY);",
		"data/t_z.sql": "INSERT INTO t_z (id) VALUES (1);",
	})
	err := runFS(testDB(t), fs)
	if err == nil || !strings.Contains(err.Error(), "没有对应的建表脚本") {
		t.Fatalf("无对应建表脚本的种子应报错, got: %v", err)
	}
}

func TestRunRejectsBadFilename(t *testing.T) {
	for _, name := range []string{"ddl/demo.sql", "ddl/0001_x.sql"} {
		fs := mapFS(map[string]string{
			name: "CREATE TABLE IF NOT EXISTS t_demo (id INTEGER PRIMARY KEY);",
		})
		err := runFS(testDB(t), fs)
		if err == nil || !strings.Contains(err.Error(), "不合规") {
			t.Fatalf("%s 命名不合规应报错, got: %v", name, err)
		}
	}
}

func TestRunEmptyFSOK(t *testing.T) {
	// 无脚本：空 FS 合法（成功零动作）
	if err := runFS(testDB(t), fstest.MapFS{}); err != nil {
		t.Fatalf("空 FS: %v", err)
	}
}

func TestEmbeddedSQLWellFormed(t *testing.T) {
	// 真实嵌入脚本的结构性契约（不连库）：命名/主干一致、data 都有 ddl 对应、
	// ddl 覆盖全部 10 张业务表。
	ddls, err := loadScripts(scriptFS, "sql/ddl", true)
	if err != nil {
		t.Fatalf("load ddl: %v", err)
	}
	datas, err := loadScripts(scriptFS, "sql/data", false)
	if err != nil {
		t.Fatalf("load data: %v", err)
	}
	want := map[string]bool{
		"t_tenant":               true,
		"t_system_users":         true,
		"t_system_role":          true,
		"t_system_menu":          true,
		"t_system_dept":          true,
		"t_system_post":          true,
		"t_system_user_role":     true,
		"t_system_role_menu":     true,
		"t_system_login_log":     true,
		"t_system_operation_log": true,
	}
	got := make(map[string]bool, len(ddls))
	for _, s := range ddls {
		got[s.Table] = true
	}
	if len(got) != len(ddls) {
		t.Fatalf("建表脚本表名重复: %v", ddls)
	}
	for tbl := range want {
		if !got[tbl] {
			t.Errorf("缺建表脚本 sql/ddl/%s.sql", tbl)
		}
	}
	for tbl := range got {
		if !want[tbl] {
			t.Errorf("多余建表脚本 %s", tbl)
		}
	}
	ddlSet := got
	for _, s := range datas {
		if !ddlSet[s.Table] {
			t.Errorf("种子 %s 无对应建表脚本", s.Name)
		}
	}
}
