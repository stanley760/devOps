// 定位与边界（与既有两层的分工）：
//   - Casbin 管「能不能调这个接口」（功能权限），TenantPlugin 管「租户内还是租户外」
//     （租户隔离）；本包管租户之内「能看到/操作哪些行」（数据权限），三层叠加不冲突。
//   - 本包为纯计算，不访问 DB：角色/用户/部门由调用方（middleware 解析层）查询注入，
//     dao 层经 Apply/Visible 消费，避免 dao ↔ 解析层循环依赖。
//
// 口径（对齐 yudao，t_system_role.data_scope 已预留）：
//
//	1 全部  2 自定义部门  3 本部门  4 本部门及以下  5 仅本人
//
// 多角色合并规则：最宽松的单角色生效（对齐 yudao 优先级语义，确定性可解释）；
// 同为「自定义部门」的多个角色取并集。宽容度序：全部 > 本部门及以下 > 本部门 > 自定义 > 仅本人。
//
// 注入策略——显式优于隐式：tenant_id 所有租户表都有、语义统一，适合 TenantPlugin
// 全局自动注入；数据权限的载体列（dept_id）与受控查询集合是业务语义，故只提供
// Apply/Visible 两个收口，由 dao/service 显式调用，漏配在评审与测试中可见。
package datascope

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"gorm.io/gorm"

	"devops-infra/errs"
)

// 数据权限口径常量（t_system_role.data_scope 取值）。
const (
	ScopeAll       int8 = 1 // 全部数据
	ScopeCustom    int8 = 2 // 自定义部门集合（data_scope_dept_ids，JSON 数组字符串）
	ScopeDept      int8 = 3 // 仅本部门
	ScopeDeptBelow int8 = 4 // 本部门及以下
	ScopeSelf      int8 = 5 // 仅本人
)

// Scope 解析后的行级可见范围（值对象，ctx 注入后请求内复用）。
// All 直通过滤；SelfOnly 命中 Owner 列；DeptIDs 命中 Dept 列；三者互斥（合并规则保证）。
type Scope struct {
	All      bool
	SelfOnly bool
	SelfID   int64   // SelfOnly 时的命中行主键
	DeptIDs  []int64 // 允许的部门集合；All/SelfOnly 时为空
}

// DeptNode 部门树最小视图（解析层由 model.Dept 转换，本包不依赖 model）。
type DeptNode struct {
	ID       int64
	ParentID int64
}

// RoleScope 角色的数据权限声明（解析层由 model.Role 转换）。
type RoleScope struct {
	Code             string
	DataScope        int8
	DataScopeDeptIDs string
}

// UserRef 解析数据权限所需的用户最小视图（解析层由 model.User 转换，本包不依赖 model）。
type UserRef struct {
	ID     int64
	DeptID int64
}

// ParseDeptIDs 解析 data_scope_dept_ids（JSON 数组字符串，形如 "[1,2,3]"，
// 与 post_ids 存储格式一致）。非法/空输入返回空集，不报错——校验归属 Role 保存路径。
func ParseDeptIDs(s string) []int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return nil
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// NormalizeDeptIDs 归一化部门 ID 集合的存储形态：解析后以数字数组重新 marshal
// （空集返回 ""，调用方写 NULL）。客户端入参可能带空串/空白，直接透传会让
// 同一语义在库里出现多种写法；落库前统一，读侧 ParseDeptIDs 不受影响。
func NormalizeDeptIDs(s string) string {
	ids := ParseDeptIDs(s)
	if len(ids) == 0 {
		return ""
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(b)
}

// ChildrenMap 部门列表 → 父 ID 到子 ID 集合的邻接表（子树展开用）。
func ChildrenMap(depts []DeptNode) map[int64][]int64 {
	children := make(map[int64][]int64, len(depts))
	for _, d := range depts {
		children[d.ParentID] = append(children[d.ParentID], d.ID)
	}
	return children
}

// ExpandSubtree 自根集合出发 BFS 展开子树（含根自身）。
func ExpandSubtree(children map[int64][]int64, roots []int64) []int64 {
	if len(roots) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(children))
	queue := append([]int64(nil), roots...)
	for _, r := range roots {
		seen[r] = struct{}{}
	}
	for i := 0; i < len(queue); i++ {
		for _, child := range children[queue[i]] {
			if _, ok := seen[child]; !ok {
				seen[child] = struct{}{}
				queue = append(queue, child)
			}
		}
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Merge 多角色合并（最宽松单角色生效；同宽容度的自定义角色取并集）。
// ownDeptID 为当前用户部门（0 = 无部门：本部门/及以下口径退化为不可见任何部门），
// selfID 为当前用户主键（仅本人口径的命中行），children 为租户部门树邻接表。
func Merge(roles []RoleScope, ownDeptID, selfID int64, children map[int64][]int64) Scope {
	bestRank := -1 // 已遇最大宽容度；-1 = 尚无角色
	var best int8
	var customUnion []int64
	customSeen := make(map[int64]struct{})

	for _, r := range roles {
		switch r.DataScope {
		case ScopeAll:
			return Scope{All: true}
		case ScopeDeptBelow:
			if bestRank < 3 {
				best, bestRank = ScopeDeptBelow, 3
			}
		case ScopeDept:
			if bestRank < 2 {
				best, bestRank = ScopeDept, 2
			}
		case ScopeCustom:
			for _, id := range ParseDeptIDs(r.DataScopeDeptIDs) {
				if _, ok := customSeen[id]; !ok {
					customSeen[id] = struct{}{}
					customUnion = append(customUnion, id)
				}
			}
			if bestRank < 1 {
				best, bestRank = ScopeCustom, 1
			}
		case ScopeSelf:
			if bestRank < 0 {
				best, bestRank = ScopeSelf, 0
			}
		}
	}

	switch best {
	case ScopeDeptBelow:
		if ownDeptID <= 0 {
			return Scope{} // 无部门：不可见任何行（fail-closed，不含本人兜底）
		}
		return Scope{DeptIDs: ExpandSubtree(children, []int64{ownDeptID})}
	case ScopeDept:
		if ownDeptID <= 0 {
			return Scope{}
		}
		return Scope{DeptIDs: []int64{ownDeptID}}
	case ScopeCustom:
		if len(customUnion) == 0 {
			return Scope{}
		}
		sort.Slice(customUnion, func(i, j int) bool { return customUnion[i] < customUnion[j] })
		return Scope{DeptIDs: customUnion}
	default: // 无角色或全部为仅本人
		return Scope{SelfOnly: true, SelfID: selfID}
	}
}

// ---- ctx 注入/读取 ----

// CtxKey 数据权限在 ctx 中的键（导出：huma 解析中间件经 huma.WithValue 注入
// 时必须与 From/WithScope 使用同一键实例，跨包故导出类型本身）。
type CtxKey struct{}

// WithScope 把解析结果注入 ctx（解析中间件调用；dao/service 读取）。
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, CtxKey{}, s)
}

// From 取 ctx 中的数据权限（未注入时 ok=false，调用方 fail-closed）。
func From(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(CtxKey{}).(Scope)
	return s, ok
}

// Columns 受控查询的载体列（dept_id 与行主键在不同表/JOIN 别名下不同，显式声明）。
type Columns struct {
	Dept  string // 部门列（如 "dept_id"，JOIN 下可为 "u.dept_id"）
	Owner string // 行主键列（仅本人口径用，如 "id"）
}

// Apply 把 ctx 中的数据权限注入查询（列表类查询的收口）。
//
// fail-closed：ctx 未注入数据权限时直接给查询挂错（对齐 TenantPlugin 对缺失租户的
// fail-fast 语义），防止受控查询在漏挂解析中间件时退化为全量可见。
func Apply(ctx context.Context, db *gorm.DB, cols Columns) *gorm.DB {
	s, ok := From(ctx)
	if !ok {
		_ = db.AddError(errs.New(errs.CodeInternal, "查询缺少数据权限上下文（受控接口未挂解析中间件）"))
		return db
	}
	switch {
	case s.All:
		return db
	case s.SelfOnly:
		return db.Where(cols.Owner+" = ?", s.SelfID)
	case len(s.DeptIDs) > 0:
		return db.Where(cols.Dept+" IN ?", s.DeptIDs)
	default:
		return db.Where("1 = 0") // 空集：不可见任何行
	}
}

// Visible 单点可见性判定（按 ID 直查/改/删的收口，越权一律不可见）。
// deptID 为目标行部门、rowID 为目标行主键；调用方在取到行后判定，
// 不可见时按 404 处理（避免存在性探测，与「资源不存在」同一呈现）。
func (s Scope) Visible(deptID, rowID int64) bool {
	switch {
	case s.All:
		return true
	case s.SelfOnly:
		return rowID == s.SelfID
	default:
		return slices.Contains(s.DeptIDs, deptID)
	}
}
