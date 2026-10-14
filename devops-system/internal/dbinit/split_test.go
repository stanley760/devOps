package dbinit

import "testing"

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
