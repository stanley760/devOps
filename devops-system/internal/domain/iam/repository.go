package iam

import (
	"context"

	"github.com/stanley760/devops/devops-common/domain"
)

// 仓储端口（Repository Pattern）。
//
// 关键设计：入参一律使用本包定义的纯 Go Filter 类型，**不带任何 huma/GORM tag**。
//
// 改造前的反向依赖：6 个 dao 文件 import internal/types，方法签名形如
// ListUsers(ctx, types.UserPageQuery)，导致存储层被 huma/OpenAPI 的
// form/binding/doc 标签污染，仓储无法脱离 HTTP 契约独立演进。
// 现在 API 契约止于 handler 层——handler 做 Input → Filter 的转换，
// 仓储以下游不再认识 OpenAPI。
//
// 端口定义在领域层，实现在 infrastructure/persistence（GORM），
// 由 app 层装配注入。这也让 service 层可以注入内存假实现做单元测试，
// 彻底解决「dao 层零测试覆盖」的问题。

// UserFilter 用户列表查询条件（领域侧，纯 Go）。
type UserFilter struct {
	domain.PageParam
	Username string // 模糊匹配
	Mobile   string // 精确匹配
	Status   *int8
	DeptID   *int64
}

// RoleFilter 角色列表查询条件。
type RoleFilter struct {
	domain.PageParam
	Name   string
	Code   string
	Status *int8
}

// MenuFilter 菜单列表查询条件。
type MenuFilter struct {
	MenuType *int8
	Name     string
	Status   *int8
}

// DeptFilter 部门列表查询条件。
type DeptFilter struct {
	Name   string
	Status *int8
}

// PostFilter 岗位列表查询条件。
type PostFilter struct {
	domain.PageParam
	Code   string
	Name   string
	Status *int8
}

// TenantFilter 租户列表查询条件。
type TenantFilter struct {
	domain.PageParam
	Name   string
	Code   string
	Status *int8
}

// LoginLogFilter 登录日志查询条件。
type LoginLogFilter struct {
	domain.PageParam
	Username  string
	IP        string
	Status    *int8
	BeginTime *string
	EndTime   *string
}

// OperationLogFilter 操作日志查询条件。
type OperationLogFilter struct {
	domain.PageParam
	Title    string
	OperName string
	Method   string
	Status   *int8
}

// UserRepository 用户仓储端口。
type UserRepository interface {
	// Save 保存聚合（新增或更新），并在同一事务内落库领域事件。
	Save(ctx context.Context, u *User) error
	// Get 按 ID 加载聚合（含角色引用）。
	Get(ctx context.Context, id int64) (*User, error)
	// GetByUsername 按用户名加载（登录场景）。
	GetByUsername(ctx context.Context, username string) (*User, error)
	// List 分页查询（读侧，直接返回领域对象列表）。
	List(ctx context.Context, f UserFilter) (domain.PageResult[*User], error)
	// Delete 软删（改写唯一键 + 清关联），并落库领域事件。
	Delete(ctx context.Context, u *User, operator string) error
	// UsernameExists 用户名是否已占用（excludeID 排除自身）。
	UsernameExists(ctx context.Context, username string, excludeID int64) (bool, error)
	// MobileExists 手机号是否已占用。
	MobileExists(ctx context.Context, mobile string, excludeID int64) (bool, error)
	// ReplaceRoles 重建用户角色关联（事务内）。
	ReplaceRoles(ctx context.Context, userID int64, roleIDs []int64) error
	// RoleCodesByIDs 按角色 ID 查编码（顺带校验归属，非本租户即报错）。
	RoleCodesByIDs(ctx context.Context, roleIDs []int64) ([]string, error)
	// TouchLogin 记录登录 IP 与时间。
	TouchLogin(ctx context.Context, userID int64, ip string) error
}

// RoleRepository 角色仓储端口。
type RoleRepository interface {
	Save(ctx context.Context, r *Role) error
	Get(ctx context.Context, id int64) (*Role, error)
	List(ctx context.Context, f RoleFilter) (domain.PageResult[*Role], error)
	Delete(ctx context.Context, r *Role, operator string) error
	CodeExists(ctx context.Context, code string, excludeID int64) (bool, error)
	// ReplaceMenus 重建角色菜单关联。
	ReplaceMenus(ctx context.Context, roleID int64, menuIDs []int64) error
	// MenusByIDs 按 ID 批量查菜单（分配菜单时派生权限标识）。
	MenusByIDs(ctx context.Context, menuIDs []int64) ([]*Menu, error)
	// PermissionsByMenuIDs 批量取菜单权限标识（避免加载整个菜单聚合）。
	PermissionsByMenuIDs(ctx context.Context, menuIDs []int64) ([]string, error)
}

// MenuRepository 菜单仓储端口。
type MenuRepository interface {
	Save(ctx context.Context, m *Menu) error
	Get(ctx context.Context, id int64) (*Menu, error)
	// ListAll 全部菜单（构建树用，无分页）。
	ListAll(ctx context.Context, f MenuFilter) ([]*Menu, error)
	// ListByUserRoles 取用户在租户内可见的菜单（登录后前端路由树）。
	ListByUserRoles(ctx context.Context, userID int64, menuTypes []int8) ([]*Menu, error)
	// ListByIDs 按 ID 批量查。
	ListByIDs(ctx context.Context, ids []int64) ([]*Menu, error)
	Delete(ctx context.Context, id int64) error
}

// DeptRepository 部门仓储端口。
type DeptRepository interface {
	Save(ctx context.Context, d *Dept) error
	Get(ctx context.Context, id int64) (*Dept, error)
	List(ctx context.Context, f DeptFilter) ([]*Dept, error)
	Delete(ctx context.Context, d *Dept, operator string) error
	// HasChildren 是否存在下级部门。
	HasChildren(ctx context.Context, id int64) (bool, error)
	// HasMembers 部门下是否有用户。
	HasMembers(ctx context.Context, id int64) (bool, error)
	// LeaderNames 批量取部门负责人姓名（登录后组树展示）。
	LeaderNames(ctx context.Context, deptIDs []int64) (map[int64]string, error)
}

// PostRepository 岗位仓储端口。
type PostRepository interface {
	Save(ctx context.Context, p *Post) error
	Get(ctx context.Context, id int64) (*Post, error)
	List(ctx context.Context, f PostFilter) (domain.PageResult[*Post], error)
	Delete(ctx context.Context, p *Post, operator string) error
	CodeExists(ctx context.Context, code string, excludeID int64) (bool, error)
}

// TenantRepository 租户仓储端口。
type TenantRepository interface {
	Save(ctx context.Context, t *Tenant) error
	Get(ctx context.Context, id int64) (*Tenant, error)
	GetByCode(ctx context.Context, code string) (*Tenant, error)
	List(ctx context.Context, f TenantFilter) (domain.PageResult[*Tenant], error)
	Delete(ctx context.Context, t *Tenant, operator string) error
	CodeExists(ctx context.Context, code string, excludeID int64) (bool, error)
	// IsAnyUserExists 租户下是否已有用户（新建租户时拒绝占用已用编码）。
	IsAnyUserExists(ctx context.Context, tenantID int64) (bool, error)
}
