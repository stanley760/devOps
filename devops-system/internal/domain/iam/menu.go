package iam

import (
	"strings"
	"time"

	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/domain/shared"
)

// Menu 菜单聚合根（全局表，无 tenant_id）。
//
// 注意：t_system_menu 不带 tenant_id，属全局表，租户插件对其不做注入。
// 菜单树的构建（父子装配）见 buildTree —— 改造前有 3 份几乎逐字重复的
// 树组装代码（service/dept.go:143、service/menu.go:204、service/auth.go:267），
// 现统一为泛型 buildTree。
type Menu struct {
	shared.AggregateRoot

	ID            int64
	ParentID      int64
	Name          string
	Type          int8
	Path          string
	Component     string
	ComponentName string
	// Permission 权限标识，如 system:user:create；目录与租户管理留空。
	// 命名与列名 permission 对齐（避免与标准库 Permission 常量混淆，
	// 故不用简称 Perm）。
	Permission string
	Icon       string
	Sort       int
	Visible    bool
	KeepAlive  bool
	AlwaysShow bool
	Status     int8
	// Remark 非表列：菜单表无 remark 列，此字段仅用于承载输入并被仓储忽略，
	// 保留以对齐既有 API 契约（types.MenuSaveReq 含 remark）。
	CreateTime time.Time
	UpdateTime time.Time

	// Children 子菜单（聚合内引用）。
	Children []*Menu
}

// MenuType 菜单类型。
const (
	MenuTypeDir    int8 = 1 // 目录
	MenuTypeMenu   int8 = 2 // 菜单
	MenuTypeButton int8 = 3 // 按钮
)

// MenuUpdate 菜单变更入参（领域输入）。
type MenuUpdate struct {
	Name          string
	Type          int8
	ParentID      int64
	Path          string
	Component     string
	ComponentName string
	Permission    string
	Icon          string
	Sort          int
	Visible       bool
	KeepAlive     bool
	AlwaysShow    bool
	Status        int8
	Remark        string
}

// ValidateForCreate 创建前校验：名称非空、父节点非自引用。
func (m *Menu) ValidateForCreate() error {
	if strings.TrimSpace(m.Name) == "" {
		return errs.New(errs.CodeParam, "菜单名称不能为空")
	}
	if m.ParentID == m.ID {
		return errs.New(errs.CodeParam, "上级菜单不能是自己")
	}
	return nil
}

// Change 应用变更（不含菜单类型变更，类型变更会影响子节点语义）。
func (m *Menu) Change(up MenuUpdate) {
	m.Name = up.Name
	m.Type = up.Type
	m.ParentID = up.ParentID
	m.Path = up.Path
	m.Component = up.Component
	m.ComponentName = up.ComponentName
	m.Permission = up.Permission
	m.Icon = up.Icon
	m.Sort = up.Sort
	m.Visible = up.Visible
	m.KeepAlive = up.KeepAlive
	m.AlwaysShow = up.AlwaysShow
	m.Status = up.Status
}

// EnsureNotSelfOrDescendant 校验菜单不可挂到自己的子树之下（防环）。
//
// 该规则在改造前不存在——原 service/menu.go 仅在聚合加载失败时兜底，
// 存在把菜单挂到自身后代导致树构建死循环的风险。此处显式建模为领域规则。
func (m *Menu) EnsureNotSelfOrDescendant(parentID int64, all []*Menu) error {
	if parentID == 0 || parentID == m.ID {
		return nil
	}
	byID := make(map[int64]*Menu, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}
	// 从目标父节点向上回溯，若途中经过自身即为成环。
	seen := make(map[int64]bool, len(all))
	for cur := parentID; cur != 0; {
		if cur == m.ID {
			return errs.New(errs.CodeParam, "上级菜单不能是自己的下级")
		}
		if seen[cur] {
			break // 存量数据已有环，停止回溯避免死循环
		}
		seen[cur] = true
		parent, ok := byID[cur]
		if !ok {
			return nil
		}
		cur = parent.ParentID
	}
	return nil
}

// CollectPermissions 收集自身与全部后代的权限标识（分配角色菜单时派生 p 行用）。
func (m *Menu) CollectPermissions() []string {
	out := make([]string, 0)
	var walk func(*Menu)
	walk = func(x *Menu) {
		if p := strings.TrimSpace(x.Permission); p != "" {
			out = append(out, p)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(m)
	return out
}

// IsButton 是否按钮类型。
func (m *Menu) IsButton() bool { return m.Type == MenuTypeButton }
