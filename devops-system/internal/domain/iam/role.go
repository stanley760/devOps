package iam

import (
	"strings"
	"time"

	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-system/internal/domain/shared"
)

// 角色类型。
const (
	// RoleTypeBuiltin 内置角色（如 super_admin）：不派生 p 行，域内通配由种子赋予。
	// 业务规则：内置角色不可删除、菜单分配不覆盖其通配权限。
	RoleTypeBuiltin int8 = 1
	// RoleTypeCustom 自定义角色：菜单分配派生 p 行。
	RoleTypeCustom int8 = 2
)

// 数据权限口径（与 devops-infra/datascope 的常量口径一致）。
const (
	DataScopeAll        int8 = 1 // 全部数据权限
	DataScopeCustom     int8 = 2 // 自定义（须携带部门集合）
	DataScopeDefaultDef int8 = 1 // 零值时的默认口径
)

// Role 角色聚合根。
//
// 充血模型收进的规则（改造前散落于 service/role.go）：
//   - code 不能为空（role.go:72）
//   - data_scope 为自定义时必须携带非空部门集合（role.go:82 → validateDataScope）
//   - code 创建后不可改，避免 casbin p/g 行失联（role.go:106）
//   - 内置角色不可删除（role.go:180）
//   - 内置角色菜单分配不派生 p 行（role.go:159）
type Role struct {
	shared.AggregateRoot

	ID       int64
	TenantID int64
	Name     string
	Code     string // 租户内唯一；casbin p/g 行的关联键，创建后不可变
	Sort     int
	// DataScope 数据权限口径。
	DataScope int8
	// DataScopeDeptIDs 自定义口径的部门集合（数字数组 JSON 字符串）。
	DataScopeDeptIDs string
	Status           int8
	Type             int8
	Remark           string
	CreateTime       time.Time
	UpdateTime       time.Time

	// MenuIDs 已分配菜单（聚合内引用）。
	MenuIDs []int64
}

// IsBuiltin 是否内置角色。
func (r *Role) IsBuiltin() bool { return r.Type == RoleTypeBuiltin }

// ValidateForCreate 创建前校验：code 非空 + 数据权限口径合法。
func (r *Role) ValidateForCreate() error {
	if strings.TrimSpace(r.Code) == "" {
		return errs.New(errs.CodeParam, "角色编码不能为空")
	}
	if err := ValidateDataScope(r.DataScope, r.DataScopeDeptIDs); err != nil {
		return err
	}
	return nil
}

// ValidateDataScope 数据权限口径校验（领域规则）。
//
// 自定义口径必须携带可解析的非空部门集合。改造前是 service/role.go:24 的
// 包级私有函数 validateDataScope，此处提升为领域规则，便于独立单测。
func ValidateDataScope(dataScope int8, deptIDs string) error {
	if dataScope != DataScopeCustom {
		return nil
	}
	if len(ParseDeptIDs(deptIDs)) == 0 {
		return errs.New(errs.CodeParam, "自定义部门口径必须提供部门 ID 集合（data_scope_dept_ids，JSON 数组）")
	}
	return nil
}

// RoleUpdate 角色变更入参（领域输入）。
//
// Code 不在入参内：编码创建后不可变，避免 casbin 策略失联。
// 改造前 service/role.go:126-134 的 updates map 靠「不写 code 键」隐式忽略，
// 隐式约定易被误改，此处由类型层面根除该风险。
type RoleUpdate struct {
	Name             string
	Sort             int
	DataScope        int8
	DataScopeDeptIDs string
	Status           int8
	Remark           string
}

// Change 应用变更。
func (r *Role) Change(up RoleUpdate, at time.Time) {
	r.Name = up.Name
	r.Sort = up.Sort
	r.DataScope = up.DataScope
	r.DataScopeDeptIDs = up.DataScopeDeptIDs
	r.Status = up.Status
	r.Remark = up.Remark
	r.UpdateTime = at
}

// ResolveUpdate 合并更新入参与存量值，返回最终生效的字段集。
//
// 规则：data_scope 传 0 时保留存量；口径未变且未传部门集合时保留存量部门集合
// （该语义来自 service/role.go:112-122，属业务规则而非存储细节，故置于领域层）。
func (r *Role) ResolveUpdate(up RoleUpdate, normalizeDeptIDs func(string) string) RoleUpdate {
	if up.DataScope == 0 {
		up.DataScope = r.DataScope
	}
	if up.DataScopeDeptIDs == "" && up.DataScope == r.DataScope {
		up.DataScopeDeptIDs = r.DataScopeDeptIDs
	}
	up.DataScopeDeptIDs = normalizeDeptIDs(up.DataScopeDeptIDs)
	return up
}

// AssignMenus 分配菜单，触发 p 行重建事件。
//
// 内置角色不派生 p 行：其通配权限（*, {tid}, *）由种子赋予，不随菜单变化，
// 否则会把超管锁死。此判断原先在 service/role.go:159，现为领域规则。
func (r *Role) AssignMenus(menuIDs []int64, permissions []string, at time.Time) {
	r.MenuIDs = menuIDs
	if r.IsBuiltin() {
		return // 内置角色保持域内通配
	}
	r.RecordEvent(RoleMenusAssigned{
		BaseEvent:   shared.BaseEvent{Time: at},
		RoleID:      r.ID,
		RoleCode:    r.Code,
		TenantID:    r.TenantID,
		Permissions: permissions,
	})
}

// EnsureDeletable 删除前置校验：内置角色不可删。
func (r *Role) EnsureDeletable() error {
	if r.IsBuiltin() {
		return errs.New(errs.CodeParam, "内置角色不可删除")
	}
	return nil
}

// MarkDeleted 软删并记录清理事件。
func (r *Role) MarkDeleted(at time.Time) {
	r.RecordEvent(RoleDeleted{
		BaseEvent: shared.BaseEvent{Time: at},
		RoleID:    r.ID,
		RoleCode:  r.Code,
		TenantID:  r.TenantID,
	})
}

// ParseDeptIDs 解析部门 ID 集合（数字数组 JSON）。
//
// 容错口径与既有实现一致：非法 JSON 返回空集，不做兼容读。
func ParseDeptIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return decodeInt64JSONArray(raw)
}
