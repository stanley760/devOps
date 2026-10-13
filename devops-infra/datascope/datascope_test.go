// datascope 纯计算层单测：口径合并矩阵、部门树展开、Apply SQL 注入（sqlite 内存库）、
// 单点可见性与 fail-closed。
package datascope_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/stanley760/devOps/devops-infra/datascope"
)

func TestParseDeptIDs(t *testing.T) {
	cases := []struct {
		name, in string
		want     []int64
	}{
		{"JSON 数组", "[1,2,3]", []int64{1, 2, 3}},
		{"空串", "", nil},
		{"非法 JSON", "1,2,3", nil},
		{"非正数过滤", "[0,-1,5]", []int64{5}},
		{"空数组", "[]", []int64{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := datascope.ParseDeptIDs(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("ParseDeptIDs(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("ParseDeptIDs(%q) = %v, want %v", c.in, got, c.want)
				}
			}
		})
	}
}

func TestNormalizeDeptIDs(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"数字数组原样", "[1,2,3]", "[1,2,3]"},
		{"空白容忍", "  [1,2]  ", "[1,2]"},
		{"非正数过滤后重编", "[0,-1,5]", "[5]"},
		{"空串", "", ""},
		{"空数组", "[]", ""},
		{"非法输入", "abc", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := datascope.NormalizeDeptIDs(c.in); got != c.want {
				t.Fatalf("NormalizeDeptIDs(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestExpandSubtree(t *testing.T) {
	// 树： 1 → 2 → 4, 2 → 5, 3（独立子树）
	children := datascope.ChildrenMap([]datascope.DeptNode{
		{ID: 2, ParentID: 1}, {ID: 3, ParentID: 0}, {ID: 4, ParentID: 2}, {ID: 5, ParentID: 2},
	})
	got := datascope.ExpandSubtree(children, []int64{1})
	want := []int64{1, 2, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("ExpandSubtree = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExpandSubtree = %v, want %v", got, want)
		}
	}
	// 展开不越界到兄弟子树
	got = datascope.ExpandSubtree(children, []int64{2})
	if len(got) != 3 || got[0] != 2 || got[1] != 4 || got[2] != 5 {
		t.Fatalf("ExpandSubtree(2) = %v, want [2 4 5]", got)
	}
}

func TestMerge(t *testing.T) {
	children := datascope.ChildrenMap([]datascope.DeptNode{
		{ID: 10, ParentID: 0}, {ID: 11, ParentID: 10}, {ID: 20, ParentID: 0},
	})
	cases := []struct {
		name      string
		roles     []datascope.RoleScope
		ownDept   int64
		wantAll   bool
		wantSelf  bool
		wantDepts []int64
	}{
		{"无角色 → 仅本人", nil, 10, false, true, nil},
		{"任一角色全部 → 全部", []datascope.RoleScope{
			{DataScope: datascope.ScopeSelf}, {DataScope: datascope.ScopeAll},
		}, 10, true, false, nil},
		{"及以下压过仅本人（子树展开）", []datascope.RoleScope{
			{DataScope: datascope.ScopeSelf}, {DataScope: datascope.ScopeDeptBelow},
		}, 10, false, false, []int64{10, 11}},
		{"本部门压过自定义", []datascope.RoleScope{
			{DataScope: datascope.ScopeCustom, DataScopeDeptIDs: "[20]"},
			{DataScope: datascope.ScopeDept},
		}, 10, false, false, []int64{10}},
		{"自定义压过仅本人", []datascope.RoleScope{
			{DataScope: datascope.ScopeSelf},
			{DataScope: datascope.ScopeCustom, DataScopeDeptIDs: "[20]"},
		}, 10, false, false, []int64{20}},
		{"多个自定义取并集（有序）", []datascope.RoleScope{
			{DataScope: datascope.ScopeCustom, DataScopeDeptIDs: "[20]"},
			{DataScope: datascope.ScopeCustom, DataScopeDeptIDs: "[11,10]"},
		}, 10, false, false, []int64{10, 11, 20}},
		{"无部门的本部门 → 不可见任何行", []datascope.RoleScope{{DataScope: datascope.ScopeDept}}, 0, false, false, nil},
		{"无部门的及以下 → 不可见任何行", []datascope.RoleScope{{DataScope: datascope.ScopeDeptBelow}}, 0, false, false, nil},
		{"自定义但部门集合非法 → 不可见任何行", []datascope.RoleScope{{DataScope: datascope.ScopeCustom, DataScopeDeptIDs: "abc"}}, 10, false, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := datascope.Merge(c.roles, c.ownDept, 77, children)
			if got.All != c.wantAll {
				t.Fatalf("All = %v, want %v", got.All, c.wantAll)
			}
			if got.SelfOnly != c.wantSelf {
				t.Fatalf("SelfOnly = %v, want %v", got.SelfOnly, c.wantSelf)
			}
			if c.wantSelf && got.SelfID != 77 {
				t.Fatalf("SelfID = %d, want 77", got.SelfID)
			}
			if len(got.DeptIDs) != len(c.wantDepts) {
				t.Fatalf("DeptIDs = %v, want %v", got.DeptIDs, c.wantDepts)
			}
			for i := range c.wantDepts {
				if got.DeptIDs[i] != c.wantDepts[i] {
					t.Fatalf("DeptIDs = %v, want %v", got.DeptIDs, c.wantDepts)
				}
			}
		})
	}
}

func TestVisible(t *testing.T) {
	cases := []struct {
		name          string
		scope         datascope.Scope
		deptID, rowID int64
		want          bool
	}{
		{"全部恒可见", datascope.Scope{All: true}, 999, 888, true},
		{"仅本人命中自身", datascope.Scope{SelfOnly: true, SelfID: 7}, 10, 7, true},
		{"仅本人不可见他人", datascope.Scope{SelfOnly: true, SelfID: 7}, 10, 8, false},
		{"部门集合内可见", datascope.Scope{DeptIDs: []int64{10, 11}}, 11, 99, true},
		{"部门集合外不可见", datascope.Scope{DeptIDs: []int64{10, 11}}, 20, 99, false},
		{"空集一律不可见", datascope.Scope{}, 10, 7, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.scope.Visible(c.deptID, c.rowID); got != c.want {
				t.Fatalf("Visible(%d, %d) = %v, want %v", c.deptID, c.rowID, got, c.want)
			}
		})
	}
}

// applyDB sqlite 内存库（裸表 users：id/dept_id，绕开 model 依赖聚焦 SQL 行为）。
func applyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:datascope_test?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable("users"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, dept_id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	for i, dept := range map[int64]int64{1: 10, 2: 11, 3: 20, 4: 20, 5: 30} {
		if err := db.Exec("INSERT INTO users (id, dept_id) VALUES (?, ?)", i, dept).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func countIDs(t *testing.T, db *gorm.DB) []int64 {
	t.Helper()
	var ids []int64
	if err := db.Table("users").Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestApply(t *testing.T) {
	cols := datascope.Columns{Dept: "dept_id", Owner: "id"}
	cases := []struct {
		name  string
		scope datascope.Scope
		want  []int64
	}{
		{"全部 → 不过滤", datascope.Scope{All: true}, []int64{1, 2, 3, 4, 5}},
		{"仅本人 → owner 命中", datascope.Scope{SelfOnly: true, SelfID: 3}, []int64{3}},
		{"部门集合 → IN 过滤", datascope.Scope{DeptIDs: []int64{10, 11}}, []int64{1, 2}},
		{"空集 → 不可见任何行", datascope.Scope{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := applyDB(t)
			ctx := datascope.WithScope(context.Background(), c.scope)
			got := countIDs(t, datascope.Apply(ctx, db, cols))
			if len(got) != len(c.want) {
				t.Fatalf("rows = %v, want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("rows = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestApplyFailClosedWithoutScope(t *testing.T) {
	db := applyDB(t)
	// ctx 未注入数据权限：查询必须失败（而非退化为全量可见）
	var ids []int64
	err := datascope.Apply(context.Background(), db, datascope.Columns{Dept: "dept_id", Owner: "id"}).
		Table("users").Pluck("id", &ids).Error
	if err == nil {
		t.Fatalf("缺少数据权限上下文时查询应报错, got rows %v", ids)
	}
}
