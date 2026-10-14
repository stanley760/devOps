// Package query 是 CQRS 读侧：查询定义与处理器。
//
// 读侧刻意绕过聚合：查询不触发业务规则、不产生副作用，直接读读模型并组装
// 视图 DTO。这正是 CQRS 的价值——把「写要守不变量」与「读要快且可自由 JOIN」
// 两件冲突的事分开，各用各的最优形态。
//
// 契约边界：本包的输出类型是 API 契约 DTO（internal/types），JSON 形态由
// huma 反射生成 OpenAPI。领域层与仓储层不认识这些 DTO（见 internal/types
// 包的单一 ID 契约说明）——依赖方向是 query → types，而非反向。
package query

import (
	"sort"
)

// TreeNode 泛型树节点。
//
// 用途：消除改造前 3 份逐字重复的树组装代码——
// service/dept.go:143-164（部门树）、service/menu.go:204-225（菜单树）、
// service/auth.go:267-293（前端路由树），三者的判定式
// `if parent, ok := byID[x.ParentID]; ok && x.ParentID != x.ID` 完全相同。
//
// 现在统一为 BuildTree 一个实现。
type TreeNode[T any] struct {
	// ID 节点 ID（扁平列表中的唯一键）。
	ID int64
	// ParentID 父节点 ID；0 表示根。
	ParentID int64
	// Order 排序值；为 0 时按加载顺序稳定排列。
	Order int
	// Value 节点载荷（DTO 指针）。
	Value T
	// Children 子节点。
	Children []TreeNode[T]
}

// ptrNode 是装配阶段的中间节点（仅内部使用）。
//
// 必须用指针而非 TreeNode 值：若在挂载时对节点做值拷贝，父节点里存下的会是
// 「挂载那一刻」的快照，其后续才挂上的子孙不会出现在该副本中
// （典型症状：孙节点丢失）。
type ptrNode[T any] struct {
	id       int64
	parentID int64
	order    int
	value    T
	children []*ptrNode[T]
}

// sortPtrRoots 根节点排序：Order 升序，0 视为「不参与排序」保持原序。
//
// 用稳定排序：Order 为 0 表示未指定顺序，此时保持原加载顺序，
// 与既有三处树实现的 `sort ASC` 口径一致。
func sortPtrRoots[T any](roots []*ptrNode[T]) {
	if len(roots) < 2 {
		return
	}
	sort.SliceStable(roots, func(i, j int) bool {
		a, b := roots[i].order, roots[j].order
		if a == 0 || b == 0 {
			return false
		}
		return a < b
	})
}

// BuildTree 由扁平列表装配树。
//
// 口径（与既有三处实现逐字对齐）：
//   - 父节点不存在于列表中，或父节点是自己 → 该节点归入根层级；
//   - 根节点按 Order 升序，Order 为 0 的节点保持原加载顺序（稳定）。
func BuildTree[T any](items []T, idOf func(T) int64, parentOf func(T) int64, orderOf func(T) int) []TreeNode[T] {
	byID := make(map[int64]*ptrNode[T], len(items))
	order := make([]int64, 0, len(items))
	for _, it := range items {
		id := idOf(it)
		n := &ptrNode[T]{id: id, parentID: parentOf(it), order: orderOf(it), value: it}
		byID[id] = n
		order = append(order, id)
	}

	roots := make([]*ptrNode[T], 0, len(items))
	for _, id := range order {
		n := byID[id]
		parent, ok := byID[n.parentID]
		if ok && n.parentID != n.id {
			parent.children = append(parent.children, n)
			continue
		}
		roots = append(roots, n)
	}

	sortPtrRoots(roots)

	var walk func(n *ptrNode[T]) TreeNode[T]
	walk = func(n *ptrNode[T]) TreeNode[T] {
		out := TreeNode[T]{
			ID:       n.id,
			ParentID: n.parentID,
			Order:    n.order,
			Value:    n.value,
		}
		if len(n.children) > 0 {
			out.Children = make([]TreeNode[T], 0, len(n.children))
			for _, c := range n.children {
				out.Children = append(out.Children, walk(c))
			}
		}
		return out
	}

	out := make([]TreeNode[T], 0, len(roots))
	for _, r := range roots {
		out = append(out, walk(r))
	}
	return out
}

// sortRoots 根节点排序：Order 升序，0 视为「不参与排序」保持原序。
//
// 用稳定排序而非插入排序，语义更直观且根节点数量小，无需手写排序算法。
func sortRoots[T any](roots []*TreeNode[T]) {
	if len(roots) < 2 {
		return
	}
	sort.SliceStable(roots, func(i, j int) bool {
		a, b := roots[i].Order, roots[j].Order
		if a == 0 || b == 0 {
			return false // 有一方未指定顺序时保持原序
		}
		return a < b
	})
}

// FlattenTree 把树拍平回列表（部分接口需要平铺形态）。
func FlattenTree[T any](nodes []TreeNode[T]) []T {
	out := make([]T, 0, len(nodes))
	var walk func([]TreeNode[T])
	walk = func(ns []TreeNode[T]) {
		for _, n := range ns {
			out = append(out, n.Value)
			walk(n.Children)
		}
	}
	walk(nodes)
	return out
}
