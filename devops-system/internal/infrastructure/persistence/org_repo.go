package persistence

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-common/domain"
	"github.com/stanley760/devOps/devops-common/domain/gormx"
	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/domain/iam"
	"github.com/stanley760/devOps/devops-system/internal/model"
)

// menuRepo 是 iam.MenuRepository 的 GORM 实现。
//
// t_system_menu 是全局表（无 tenant_id），租户插件对其不做注入，
// 菜单本身由全平台统一维护。
type menuRepo struct{}

// NewMenuRepository 构造菜单仓储。
func NewMenuRepository() iam.MenuRepository { return menuRepo{} }

func (menuRepo) Save(ctx context.Context, m *iam.Menu) error {
	if m.ID == 0 {
		if err := db(ctx).Create(toMenuPO(m)).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建菜单失败")
		}
		return nil
	}
	res := db(ctx).Model(&model.Menu{}).Where("id = ?", m.ID).Updates(map[string]any{
		"name":           m.Name,
		"type":           m.Type,
		"parent_id":      m.ParentID,
		"path":           m.Path,
		"component":      m.Component,
		"component_name": m.ComponentName,
		"permission":     m.Permission,
		"icon":           m.Icon,
		"sort":           m.Sort,
		"visible":        m.Visible,
		"keep_alive":     m.KeepAlive,
		"always_show":    m.AlwaysShow,
		"status":         m.Status,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新菜单失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "菜单不存在")
	}
	return nil
}

func (menuRepo) Get(ctx context.Context, id int64) (*iam.Menu, error) {
	var po model.Menu
	if err := db(ctx).Where("id = ?", id).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "菜单不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	return toMenuDomain(&po), nil
}

func (menuRepo) ListAll(ctx context.Context, f iam.MenuFilter) ([]*iam.Menu, error) {
	conn := db(ctx).Model(&model.Menu{})
	if f.MenuType != nil {
		conn = conn.Where("type = ?", *f.MenuType)
	}
	if f.Name != "" {
		conn = conn.Where("name LIKE ?", "%"+f.Name+"%")
	}
	if f.Status != nil {
		conn = conn.Where("status = ?", *f.Status)
	}
	var pos []model.Menu
	if err := conn.Order("sort ASC, id ASC").Find(&pos).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	return toMenuDomains(pos), nil
}

// ListByUserRoles 取用户在租户内可见的菜单（登录后前端路由树）。
//
// 经 user_role → role_menu 两级关联取菜单，是既有实现的双 JOIN 口径。
// 因涉及 JOIN，租户插件不做注入（见 devops-infra/tenant 插件的 JOIN 放行规则），
// 故必须显式带 tenant_id——这是防跨租户泄漏的关键点，勿删。
func (r menuRepo) ListByUserRoles(ctx context.Context, userID int64, menuTypes []int8) ([]*iam.Menu, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	conn := db(ctx).Table("t_system_menu AS m").
		Joins("JOIN t_system_role_menu rm ON rm.menu_id = m.id AND rm.tenant_id = ?", tid).
		Joins("JOIN t_system_user_role ur ON ur.role_id = rm.role_id AND ur.tenant_id = ?", tid).
		Where("ur.user_id = ?", userID)
	if len(menuTypes) > 0 {
		conn = conn.Where("m.type IN ?", menuTypes)
	}

	var pos []model.Menu
	if err := conn.Order("m.sort ASC, m.id ASC").Find(&pos).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户菜单失败")
	}
	return toMenuDomains(pos), nil
}

func (menuRepo) ListByIDs(ctx context.Context, ids []int64) ([]*iam.Menu, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var pos []model.Menu
	if err := db(ctx).Where("id IN ?", ids).Find(&pos).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询菜单失败")
	}
	return toMenuDomains(pos), nil
}

// Delete 删除菜单并清理角色关联。
func (menuRepo) Delete(ctx context.Context, id int64) error {
	conn := db(ctx)
	if err := conn.Where("menu_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除菜单失败")
	}
	if err := conn.Where("id = ?", id).Delete(&model.Menu{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除菜单失败")
	}
	return nil
}

func toMenuPO(m *iam.Menu) *model.Menu {
	return &model.Menu{
		BaseModel:     model.BaseModel{Updater: m.Name},
		Name:          m.Name,
		Permission:    m.Permission,
		Type:          m.Type,
		Sort:          m.Sort,
		ParentID:      m.ParentID,
		Path:          m.Path,
		Icon:          m.Icon,
		Component:     m.Component,
		ComponentName: m.ComponentName,
		Status:        m.Status,
		Visible:       m.Visible,
		KeepAlive:     m.KeepAlive,
		AlwaysShow:    m.AlwaysShow,
	}
}

func toMenuDomain(po *model.Menu) *iam.Menu {
	return &iam.Menu{
		ID:            po.ID,
		ParentID:      po.ParentID,
		Name:          po.Name,
		Type:          po.Type,
		Path:          po.Path,
		Component:     po.Component,
		ComponentName: po.ComponentName,
		Permission:    po.Permission,
		Icon:          po.Icon,
		Sort:          po.Sort,
		Visible:       po.Visible,
		KeepAlive:     po.KeepAlive,
		AlwaysShow:    po.AlwaysShow,
		Status:        po.Status,
		CreateTime:    po.CreateTime,
		UpdateTime:    po.UpdateTime,
	}
}

func toMenuDomains(pos []model.Menu) []*iam.Menu {
	out := make([]*iam.Menu, 0, len(pos))
	for i := range pos {
		out = append(out, toMenuDomain(&pos[i]))
	}
	return out
}

// deptRepo 是 iam.DeptRepository 的 GORM 实现。
type deptRepo struct{}

// NewDeptRepository 构造部门仓储。
func NewDeptRepository() iam.DeptRepository { return deptRepo{} }

func (deptRepo) Save(ctx context.Context, d *iam.Dept) error {
	if d.ID == 0 {
		if err := db(ctx).Create(toDeptPO(d)).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建部门失败")
		}
		return nil
	}
	res := db(ctx).Model(&model.Dept{}).Where("id = ?", d.ID).Updates(map[string]any{
		"parent_id":      d.ParentID,
		"name":           d.Name,
		"sort":           d.Sort,
		"leader_user_id": d.LeaderUserID,
		"phone":          d.Phone,
		"email":          d.Email,
		"status":         d.Status,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新部门失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "部门不存在")
	}
	return nil
}

func (deptRepo) Get(ctx context.Context, id int64) (*iam.Dept, error) {
	var po model.Dept
	if err := db(ctx).Where("id = ?", id).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "部门不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询部门失败")
	}
	return toDeptDomain(&po), nil
}

func (deptRepo) List(ctx context.Context, f iam.DeptFilter) ([]*iam.Dept, error) {
	conn := db(ctx).Model(&model.Dept{})
	if f.Name != "" {
		conn = conn.Where("name LIKE ?", "%"+f.Name+"%")
	}
	if f.Status != nil {
		conn = conn.Where("status = ?", *f.Status)
	}
	var pos []model.Dept
	if err := conn.Order("sort ASC, id ASC").Find(&pos).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询部门失败")
	}
	out := make([]*iam.Dept, 0, len(pos))
	for i := range pos {
		out = append(out, toDeptDomain(&pos[i]))
	}
	return out, nil
}

// Delete 软删部门。
func (deptRepo) Delete(ctx context.Context, d *iam.Dept, operator string) error {
	if err := db(ctx).Where("id = ?", d.ID).Delete(&model.Dept{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除部门失败")
	}
	return nil
}

func (deptRepo) HasChildren(ctx context.Context, id int64) (bool, error) {
	var cnt int64
	if err := db(ctx).Model(&model.Dept{}).
		Where("parent_id = ?", id).Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询下级部门失败")
	}
	return cnt > 0, nil
}

func (deptRepo) HasMembers(ctx context.Context, id int64) (bool, error) {
	var cnt int64
	if err := db(ctx).Model(&model.User{}).
		Where("dept_id = ?", id).Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询部门成员失败")
	}
	return cnt > 0, nil
}

// LeaderNames 批量取部门负责人姓名。
func (deptRepo) LeaderNames(ctx context.Context, deptIDs []int64) (map[int64]string, error) {
	if len(deptIDs) == 0 {
		return nil, nil
	}
	type row struct {
		ID       int64
		Nickname string
	}
	var rows []row
	// 一次查询取回所有负责人姓名，避免逐部门 N+1。
	err := db(ctx).Model(&model.User{}).
		Where("id IN ?", deptIDs).Select("id, nickname").Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询部门负责人失败")
	}
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Nickname
	}
	return out, nil
}

func toDeptPO(d *iam.Dept) *model.Dept {
	return &model.Dept{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Updater: d.Name},
			TenantID:  d.TenantID,
		},
		Name:         d.Name,
		ParentID:     d.ParentID,
		Sort:         d.Sort,
		LeaderUserID: d.LeaderUserID,
		Phone:        d.Phone,
		Email:        d.Email,
		Status:       d.Status,
	}
}

func toDeptDomain(po *model.Dept) *iam.Dept {
	return &iam.Dept{
		ID:           po.ID,
		TenantID:     po.TenantID,
		ParentID:     po.ParentID,
		Name:         po.Name,
		Sort:         po.Sort,
		LeaderUserID: po.LeaderUserID,
		Phone:        po.Phone,
		Email:        po.Email,
		Status:       po.Status,
		CreateTime:   po.CreateTime,
		UpdateTime:   po.UpdateTime,
	}
}

// postRepo 是 iam.PostRepository 的 GORM 实现。
type postRepo struct{}

// NewPostRepository 构造岗位仓储。
func NewPostRepository() iam.PostRepository { return postRepo{} }

func (postRepo) Save(ctx context.Context, p *iam.Post) error {
	if p.ID == 0 {
		if err := db(ctx).Create(toPostPO(p)).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建岗位失败")
		}
		return nil
	}
	res := db(ctx).Model(&model.Post{}).Where("id = ?", p.ID).Updates(map[string]any{
		"code":   p.Code,
		"name":   p.Name,
		"sort":   p.Sort,
		"status": p.Status,
		"remark": p.Remark,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新岗位失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "岗位不存在")
	}
	return nil
}

func (postRepo) Get(ctx context.Context, id int64) (*iam.Post, error) {
	var po model.Post
	if err := db(ctx).Where("id = ?", id).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "岗位不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询岗位失败")
	}
	return toPostDomain(&po), nil
}

func (postRepo) List(ctx context.Context, f iam.PostFilter) (domain.PageResult[*iam.Post], error) {
	conn := db(ctx).Model(&model.Post{})
	if f.Code != "" {
		conn = conn.Where("code = ?", f.Code)
	}
	if f.Name != "" {
		conn = conn.Where("name LIKE ?", "%"+f.Name+"%")
	}
	if f.Status != nil {
		conn = conn.Where("status = ?", *f.Status)
	}
	page, err := gormx.FindPage[model.Post](conn.Order("sort ASC, id ASC"), f.PageParam)
	if err != nil {
		return domain.PageResult[*iam.Post]{}, errs.Wrap(err, errs.CodeInternal, "查询岗位列表失败")
	}
	return domain.MapPage(page, func(po model.Post) *iam.Post { return toPostDomain(&po) }), nil
}

// Delete 软删岗位并改写 code 绕开唯一索引。
func (postRepo) Delete(ctx context.Context, p *iam.Post, operator string) error {
	return db(ctx).Model(&model.Post{}).Where("id = ?", p.ID).Updates(map[string]any{
		"code":       iam.SoftDeletedCode(p.Code, p.ID),
		"deleted_at": time.Now(),
		"updater":    operator,
	}).Error
}

func (postRepo) CodeExists(ctx context.Context, code string, excludeID int64) (bool, error) {
	return existsBy(ctx, &model.Post{}, "code", code, excludeID, "检查岗位编码唯一性失败")
}

func toPostPO(p *iam.Post) *model.Post {
	return &model.Post{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Updater: p.Code},
			TenantID:  p.TenantID,
		},
		Code:   p.Code,
		Name:   p.Name,
		Sort:   p.Sort,
		Status: p.Status,
		Remark: p.Remark,
	}
}

func toPostDomain(po *model.Post) *iam.Post {
	return &iam.Post{
		ID:         po.ID,
		TenantID:   po.TenantID,
		Code:       po.Code,
		Name:       po.Name,
		Sort:       po.Sort,
		Status:     po.Status,
		Remark:     po.Remark,
		CreateTime: po.CreateTime,
		UpdateTime: po.UpdateTime,
	}
}
