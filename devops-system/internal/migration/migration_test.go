package migration

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testDB 每用例独立内存库（sqlite 方言：withLock 跳过 MySQL 咨询锁路径）。
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// mapFS 构造一个 scope 目录的迁移文件系统（文件名→内容）。
func mapFS(scope Scope, files map[string]string) fstest.MapFS {
	fs := fstest.MapFS{}
	for name, content := range files {
		fs["sql/"+string(scope)+"/"+name] = &fstest.MapFile{Data: []byte(content)}
	}
	return fs
}

func TestSplitStatements(t *testing.T) {
	sql := "-- 建表 ; 注释里的分号不算\n" +
		"CREATE TABLE a (id INTEGER PRIMARY KEY, v TEXT); /* 块注释 ; 分号 */\n" +
		"INSERT INTO a (v) VALUES ('a;b');      -- 引号内分号\n" +
		"INSERT INTO a (v) VALUES ('it''s');     -- 相邻双写引号\n" +
		"INSERT INTO `tbl;x` (v) VALUES ('\\''); -- 反引号表名 + 反斜杠转义\n" +
		"-- 尾部纯注释不产出语句\n"
	want := []string{
		"CREATE TABLE a (id INTEGER PRIMARY KEY, v TEXT)",
		"INSERT INTO a (v) VALUES ('a;b')",
		"INSERT INTO a (v) VALUES ('it''s')",
		"INSERT INTO `tbl;x` (v) VALUES ('\\'')",
	}
	got := splitStatements(sql)
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stmt[%d]:\n got: %s\nwant: %s", i, got[i], want[i])
		}
	}
}

func TestRunAppliesPendingThenSkips(t *testing.T) {
	db := testDB(t)
	fs := mapFS(ScopeGlobal, map[string]string{
		"0001_create.sql": "CREATE TABLE t_demo (id INTEGER PRIMARY KEY, v TEXT);",
		"0002_insert.sql": "INSERT INTO t_demo (id, v) VALUES (1, 'a');",
	})
	if err := runScopeFS(db, ScopeGlobal, fs); err != nil {
		t.Fatalf("first run: %v", err)
	}
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM t_demo").Scan(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("rows = %d, want 1", cnt)
	}
	// 第二次执行：已应用版本直接跳过，数据不重复
	if err := runScopeFS(db, ScopeGlobal, fs); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM t_demo").Scan(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("re-run rows = %d, want 1（已应用应跳过）", cnt)
	}
}

func TestRunNewFilesAppliedOnReRun(t *testing.T) {
	db := testDB(t)
	fs := mapFS(ScopeGlobal, map[string]string{
		"0001_create.sql": "CREATE TABLE t_demo (id INTEGER PRIMARY KEY);",
	})
	if err := runScopeFS(db, ScopeGlobal, fs); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// 后续版本追加：只补跑新文件
	fs["sql/global/0002_insert.sql"] = &fstest.MapFile{Data: []byte("INSERT INTO t_demo (id) VALUES (1);")}
	if err := runScopeFS(db, ScopeGlobal, fs); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM t_demo").Scan(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("rows = %d, want 1", cnt)
	}
}

func TestRunChecksumMismatchRejected(t *testing.T) {
	db := testDB(t)
	fs := mapFS(ScopeGlobal, map[string]string{
		"0001_create.sql": "CREATE TABLE t_demo (id INTEGER PRIMARY KEY);",
	})
	if err := runScopeFS(db, ScopeGlobal, fs); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// 已应用文件被改动：checksum 不匹配必须拒绝
	fs["sql/global/0001_create.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE t_demo (id INTEGER PRIMARY KEY, v TEXT);")}
	err := runScopeFS(db, ScopeGlobal, fs)
	if err == nil {
		t.Fatal("checksum 不匹配时应报错")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("错误应提及 checksum: %v", err)
	}
}

func TestLoadRejectsBadName(t *testing.T) {
	fs := mapFS(ScopeGlobal, map[string]string{
		"seed.sql": "SELECT 1;",
	})
	err := runScopeFS(testDB(t), ScopeGlobal, fs)
	if err == nil || !strings.Contains(err.Error(), "NNNN_name.sql") {
		t.Fatalf("命名不合规应报错, got: %v", err)
	}
}

func TestLoadRejectsDuplicateVersion(t *testing.T) {
	fs := mapFS(ScopeGlobal, map[string]string{
		"0001_a.sql": "CREATE TABLE t_a (id INTEGER PRIMARY KEY);",
		"0001_b.sql": "CREATE TABLE t_b (id INTEGER PRIMARY KEY);",
	})
	err := runScopeFS(testDB(t), ScopeGlobal, fs)
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("版本号重复应报错, got: %v", err)
	}
}

func TestRunEmptyScopeOK(t *testing.T) {
	// 无迁移文件：建版本表即成功（空 scope 合法）
	if err := runScopeFS(testDB(t), ScopeGlobal, fstest.MapFS{}); err != nil {
		t.Fatalf("空 scope: %v", err)
	}
}

func TestRunRequiresScope(t *testing.T) {
	if err := Run(testDB(t)); err == nil {
		t.Fatal("无 scope 应报错")
	}
}

func TestApplyFailsAtomically(t *testing.T) {
	// 事务性：文件后半句失败时前半句必须回滚，且不记账
	db := testDB(t)
	fs := mapFS(ScopeGlobal, map[string]string{
		"0001_partial.sql": "CREATE TABLE t_demo (id INTEGER PRIMARY KEY); INSERT INTO t_demo (id) VALUES (1); INSERT INTO no_such_table (x) VALUES (1);",
	})
	if err := runScopeFS(db, ScopeGlobal, fs); err == nil {
		t.Fatal("含失败语句时应报错")
	}
	var cnt int64
	if err := db.Raw("SELECT COUNT(*) FROM t_demo").Scan(&cnt).Error; err == nil && cnt != 0 {
		t.Fatalf("失败迁移应整体回滚, rows = %d", cnt)
	}
	var applied int64
	if err := db.Raw("SELECT COUNT(*) FROM " + versionTable).Scan(&applied).Error; err != nil {
		t.Fatalf("count version table: %v", err)
	}
	if applied != 0 {
		t.Fatalf("失败迁移不应记账, got %d", applied)
	}
}
