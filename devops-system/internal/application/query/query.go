package query

import (
	"context"

	"github.com/stanley760/devops/devops-common/domain"
	"github.com/stanley760/devops/devops-infra/datascope"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-system/internal/domain/auditlog"
	"github.com/stanley760/devops/devops-system/internal/domain/iam"
	"github.com/stanley760/devops/devops-system/internal/types"
)

// 用户读侧。
//
// 与写侧的关键差异：读侧直接返回 DTO，不加载聚合、不校验不变量。
// 这让读侧可以自由地做 JOIN 与字段裁剪（列表页不需要角色明细），
// 而写侧仍严格守住聚合边界。
type UserQuery struct {
	users iam.UserRepository
}

// NewUserQuery 构造用户读侧。
func NewUserQuery(users iam.UserRepository) *UserQuery { return &UserQuery{users: users} }

// List 分页查询用户。
func (q *UserQuery) List(ctx context.Context, f iam.UserFilter) (domain.PageResult[types.UserDTO], error) {
	page, err := q.users.List(ctx, f)
	if err != nil {
		return domain.PageResult[types.UserDTO]{}, err
	}
	return domain.MapPage(page, UserDTO), nil
}

// Get 用户详情。
//
// 数据权限单点收口：按 ID 直查后判定可见性，越权一律按 404 呈现
// （与「用户不存在」同一响应，防存在性探测——此安全语义不可改为 403）。
// ctx 未注入数据权限时 fail-closed 报错（受控路由已挂解析中间件，漏配即暴露）。
func (q *UserQuery) Get(ctx context.Context, id int64) (*types.UserDetailResp, error) {
	u, err := q.users.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := EnsureVisible(ctx, u.DeptID, u.ID); err != nil {
		return nil, err
	}
	return UserDetail(u), nil
}

// GetByUsername 按用户名取详情（登录后取本人信息）。
func (q *UserQuery) GetByUsername(ctx context.Context, username string) (*types.UserDetailResp, error) {
	u, err := q.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	return UserDetail(u), nil
}

// EnsureVisible 数据权限可见性判定（单点收口）。
//
// 改造前是 service/user.go:24 的包级私有函数 ensureUserVisible，
// 由 4 个方法各自调用；现在提为导出函数供读侧统一使用。
func EnsureVisible(ctx context.Context, deptID, ownerID int64) error {
	scope, ok := datascope.From(ctx)
	if !ok {
		return errs.New(errs.CodeInternal, "缺少数据权限上下文（受控接口未挂解析中间件）")
	}
	if !scope.Visible(deptID, ownerID) {
		return errs.New(errs.CodeNotFound, "用户不存在")
	}
	return nil
}

// RoleQuery 角色读侧。
type RoleQuery struct {
	roles iam.RoleRepository
}

// NewRoleQuery 构造角色读侧。
func NewRoleQuery(roles iam.RoleRepository) *RoleQuery { return &RoleQuery{roles: roles} }

// List 分页查询角色。
func (q *RoleQuery) List(ctx context.Context, f iam.RoleFilter) (domain.PageResult[types.RoleDTO], error) {
	page, err := q.roles.List(ctx, f)
	if err != nil {
		return domain.PageResult[types.RoleDTO]{}, err
	}
	return domain.MapPage(page, RoleDTO), nil
}

// Get 角色详情（含已分配菜单 ID）。
func (q *RoleQuery) Get(ctx context.Context, id int64) (*types.RoleDetailResp, error) {
	r, err := q.roles.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return RoleDetail(r), nil
}

// MenuQuery 菜单读侧。
type MenuQuery struct {
	menus iam.MenuRepository
}

// NewMenuQuery 构造菜单读侧。
func NewMenuQuery(menus iam.MenuRepository) *MenuQuery { return &MenuQuery{menus: menus} }

// List 菜单树。
func (q *MenuQuery) List(ctx context.Context, f iam.MenuFilter) ([]types.MenuTreeResp, error) {
	all, err := q.menus.ListAll(ctx, f)
	if err != nil {
		return nil, err
	}
	return MenuTree(all), nil
}

// Tree 菜单树（无筛选）。
func (q *MenuQuery) Tree(ctx context.Context) ([]types.MenuTreeResp, error) {
	return q.List(ctx, iam.MenuFilter{})
}

// DeptQuery 部门读侧。
type DeptQuery struct {
	depts iam.DeptRepository
}

// NewDeptQuery 构造部门读侧。
func NewDeptQuery(depts iam.DeptRepository) *DeptQuery { return &DeptQuery{depts: depts} }

// List 部门树。
func (q *DeptQuery) List(ctx context.Context, f iam.DeptFilter) ([]types.DeptTreeResp, error) {
	all, err := q.depts.List(ctx, f)
	if err != nil {
		return nil, err
	}
	return DeptTree(all), nil
}

// PostQuery 岗位读侧。
type PostQuery struct {
	posts iam.PostRepository
}

// NewPostQuery 构造岗位读侧。
func NewPostQuery(posts iam.PostRepository) *PostQuery { return &PostQuery{posts: posts} }

// List 分页查询岗位。
func (q *PostQuery) List(ctx context.Context, f iam.PostFilter) (domain.PageResult[types.PostDTO], error) {
	page, err := q.posts.List(ctx, f)
	if err != nil {
		return domain.PageResult[types.PostDTO]{}, err
	}
	return domain.MapPage(page, PostDTO), nil
}

// Get 岗位详情。
func (q *PostQuery) Get(ctx context.Context, id int64) (*types.PostDTO, error) {
	p, err := q.posts.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	dto := PostDTO(p)
	return &dto, nil
}

// TenantQuery 租户读侧。
type TenantQuery struct {
	tenants iam.TenantRepository
}

// NewTenantQuery 构造租户读侧。
func NewTenantQuery(tenants iam.TenantRepository) *TenantQuery { return &TenantQuery{tenants: tenants} }

// List 分页查询租户。
func (q *TenantQuery) List(ctx context.Context, f iam.TenantFilter) (domain.PageResult[types.TenantDTO], error) {
	page, err := q.tenants.List(ctx, f)
	if err != nil {
		return domain.PageResult[types.TenantDTO]{}, err
	}
	return domain.MapPage(page, TenantDTO), nil
}

// Get 租户详情。
func (q *TenantQuery) Get(ctx context.Context, id int64) (*types.TenantDTO, error) {
	t, err := q.tenants.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	dto := TenantDTO(t)
	return &dto, nil
}

// LoginLogQuery 登录日志读侧。
type LoginLogQuery struct {
	logs auditlog.LoginLogReader
}

// NewLoginLogQuery 构造登录日志读侧。
func NewLoginLogQuery(logs auditlog.LoginLogReader) *LoginLogQuery { return &LoginLogQuery{logs: logs} }

// List 分页查询登录审计。
func (q *LoginLogQuery) List(ctx context.Context, f auditlog.LoginLogFilter) (domain.PageResult[types.LoginLogDTO], error) {
	page, err := q.logs.List(ctx, f)
	if err != nil {
		return domain.PageResult[types.LoginLogDTO]{}, err
	}
	return domain.MapPage(page, LoginLogDTO), nil
}

// OperationLogQuery 操作日志读侧。
type OperationLogQuery struct {
	logs auditlog.OperationLogReader
}

// NewOperationLogQuery 构造操作日志读侧。
func NewOperationLogQuery(logs auditlog.OperationLogReader) *OperationLogQuery {
	return &OperationLogQuery{logs: logs}
}

// List 分页查询操作审计。
func (q *OperationLogQuery) List(ctx context.Context, f auditlog.OperationLogFilter) (domain.PageResult[types.OperationLogDTO], error) {
	page, err := q.logs.List(ctx, f)
	if err != nil {
		return domain.PageResult[types.OperationLogDTO]{}, err
	}
	return domain.MapPage(page, OperationLogDTO), nil
}
