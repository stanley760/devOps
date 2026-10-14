package query

import (
	"testing"

	"github.com/stanley760/devOps/devops-common/domain"
)

// node 是树测试用的载荷。
type node struct {
	ID       int64
	ParentID int64
	Order    int
	Name     string
}

func TestBuildTree(t *testing.T) {
	items := []node{
		{ID: 1, ParentID: 0, Order: 2, Name: "root-b"},
		{ID: 2, ParentID: 0, Order: 1, Name: "root-a"},
		{ID: 3, ParentID: 1, Name: "child-of-1"},
		{ID: 4, ParentID: 3, Name: "grandchild"},
		// 父节点不存在 → 应归入根层级
		{ID: 5, ParentID: 99, Name: "orphan"},
	}

	tree := BuildTree(items,
		func(n node) int64 { return n.ID },
		func(n node) int64 { return n.ParentID },
		func(n node) int { return n.Order },
	)

	if len(tree) != 3 {
		t.Fatalf("根节点数 = %d, want 3 (1,2,5)", len(tree))
	}
	// 根按 Order 升序：2(order=1) 应在 1(order=2) 之前
	if tree[0].ID != 2 || tree[1].ID != 1 {
		t.Fatalf("根顺序 = [%d %d], want [2 1]", tree[0].ID, tree[1].ID)
	}

	// 子树挂载正确性
	var rootB *TreeNode[node]
	for i := range tree {
		if tree[i].ID == 1 {
			rootB = &tree[i]
		}
	}
	if rootB == nil {
		t.Fatal("未找到 ID=1 的根节点")
	}
	if len(rootB.Children) != 1 || rootB.Children[0].ID != 3 {
		t.Fatalf("ID=1 的子节点 = %v, want [3]", idsOf(rootB.Children))
	}
	if len(rootB.Children[0].Children) != 1 || rootB.Children[0].Children[0].ID != 4 {
		t.Fatalf("ID=3 的子节点 = %v, want [4]", idsOf(rootB.Children[0].Children))
	}
}

// TestBuildTreeSelfParentGuard 自引用节点应归入根而非死循环。
func TestBuildTreeSelfParentGuard(t *testing.T) {
	items := []node{{ID: 7, ParentID: 7, Name: "self"}}
	tree := BuildTree(items,
		func(n node) int64 { return n.ID },
		func(n node) int64 { return n.ParentID },
		func(n node) int { return n.Order },
	)
	if len(tree) != 1 || tree[0].ID != 7 {
		t.Fatalf("自引用节点应归入根，实际 %d 个根", len(tree))
	}
}

func TestFlattenTree(t *testing.T) {
	items := []node{
		{ID: 1, ParentID: 0, Name: "a"},
		{ID: 2, ParentID: 1, Name: "b"},
		{ID: 3, ParentID: 2, Name: "c"},
	}
	tree := BuildTree(items,
		func(n node) int64 { return n.ID },
		func(n node) int64 { return n.ParentID },
		func(n node) int { return n.Order },
	)
	flat := FlattenTree(tree)
	if len(flat) != 3 {
		t.Fatalf("拍平后 = %d 个, want 3", len(flat))
	}
	if flat[0].ID != 1 || flat[1].ID != 2 || flat[2].ID != 3 {
		t.Fatalf("拍平顺序 = [%d %d %d], want [1 2 3]", flat[0].ID, flat[1].ID, flat[2].ID)
	}
}

// TestBuildTreePreservesOrderZero 根节点 Order=0 时保持加载顺序。
func TestBuildTreePreservesOrderZero(t *testing.T) {
	items := []node{
		{ID: 10, ParentID: 0, Name: "first"},
		{ID: 20, ParentID: 0, Order: 5, Name: "second"},
	}
	tree := BuildTree(items,
		func(n node) int64 { return n.ID },
		func(n node) int64 { return n.ParentID },
		func(n node) int { return n.Order },
	)
	if tree[0].ID != 10 {
		t.Fatalf("首根 = %d, want 10（Order=0 应保持原序）", tree[0].ID)
	}
}

func idsOf[T any](nodes []TreeNode[T]) []int64 {
	out := make([]int64, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	return out
}

// TestPageResultMapping 分页元数据在转换中必须保持不变。
func TestPageResultMapping(t *testing.T) {
	in := domain.PageResult[int]{
		List: []int{1, 2, 3}, Total: 42, PageNum: 2, PageSize: 3, Pages: 14,
	}
	out := domain.MapPage(in, func(v int) string { return string(rune('a' + v - 1)) })
	if out.Total != in.Total || out.PageNum != in.PageNum || out.PageSize != in.PageSize || out.Pages != in.Pages {
		t.Fatalf("分页元数据未保持: %+v", out)
	}
	if len(out.List) != 3 || out.List[0] != "a" {
		t.Fatalf("列表转换错误: %v", out.List)
	}
}
