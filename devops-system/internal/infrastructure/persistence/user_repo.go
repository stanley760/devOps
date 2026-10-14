package persistence

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-common/domain"
	"github.com/stanley760/devops/devops-common/domain/gormx"
	"github.com/stanley760/devops/devops-infra/datascope"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-system/internal/domain/iam"
	"github.com/stanley760/devops/devops-system/internal/model"
)

// userRepo 是 iam.UserRepository 的 GORM 实现。
type userRepo struct{}

// NewUserRepository 构造用户仓储。
func NewUserRepository() iam.UserRepository { return userRepo{} }

// Save 保存用户聚合，并在同一事务内落库聚合记录的领域事件。
//
// 领域事件与业务行同事务写入，保证「状态已变更」与「待投影事实」原子落库。
func (r userRepo) Save(ctx context.Context, u *iam.User) error {
	conn := db(ctx)
	now := time.Now()

	if u.ID == 0 {
		u.UpdateTime = now
		if err := conn.Create(toUserPO(u)).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建用户失败")
		}
	} else {
		upd := toUserUpdates(u)
		res := conn.Model(&model.User{}).Where("id = ?", u.ID).Updates(upd)
		if res.Error != nil {
			return errs.Wrap(res.Error, errs.CodeInternal, "更新用户失败")
		}
		if res.RowsAffected == 0 {
			return errs.New(errs.CodeNotFound, "用户不存在")
		}
	}

	// 角色关联：清空后重建（同一事务）。
	if u.RoleIDs != nil {
		if err := r.replaceRolesTx(ctx, u.ID, u.RoleIDs); err != nil {
			return err
		}
	}

	// 领域事件与业务写同事务落库。
	return appendEvents(ctx, u.PullEvents()...)
}

// Get 按 ID 加载用户聚合（含角色引用）。
func (r userRepo) Get(ctx context.Context, id int64) (*iam.User, error) {
	var po model.User
	if err := db(ctx).Where("id = ?", id).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "用户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	u := toUserDomain(&po)
	roleIDs, err := r.roleIDs(ctx, po.ID)
	if err != nil {
		return nil, err
	}
	u.RoleIDs = roleIDs
	return u, nil
}

// GetByUsername 按用户名加载（登录场景）。
func (r userRepo) GetByUsername(ctx context.Context, username string) (*iam.User, error) {
	var po model.User
	if err := db(ctx).Where("username = ?", username).First(&po).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "用户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户失败")
	}
	return toUserDomain(&po), nil
}

// List 分页查询用户（行级数据权限收敛）。
//
// datascope.Apply 注入 ctx 中的可见范围；未注入时 fail-closed 报错，
// 与既有 dao.ListUsers 的安全口径一致。
func (r userRepo) List(ctx context.Context, f iam.UserFilter) (domain.PageResult[*iam.User], error) {
	conn := datascope.Apply(ctx, db(ctx).Model(&model.User{}),
		datascope.Columns{Dept: "dept_id", Owner: "id"})
	if f.Username != "" {
		conn = conn.Where("username LIKE ?", "%"+f.Username+"%")
	}
	if f.Mobile != "" {
		conn = conn.Where("mobile = ?", f.Mobile)
	}
	if f.Status != nil {
		conn = conn.Where("status = ?", *f.Status)
	}
	if f.DeptID != nil {
		conn = conn.Where("dept_id = ?", *f.DeptID)
	}

	page, err := gormx.FindPage[model.User](conn.Order("id DESC"), f.PageParam)
	if err != nil {
		return domain.PageResult[*iam.User]{}, errs.Wrap(err, errs.CodeInternal, "查询用户列表失败")
	}
	out := make([]*iam.User, 0, len(page.List))
	for i := range page.List {
		out = append(out, toUserDomain(&page.List[i]))
	}
	return domain.PageResult[*iam.User]{
		List: out, Total: page.Total, PageNum: page.PageNum,
		PageSize: page.PageSize, Pages: page.Pages,
	}, nil
}

// Delete 软删用户并清理角色关联，同时落库领域事件。
//
// 唯一键改写是存储契约：uk_tenant_username(tenant_id, username) 与
// uk_tenant_mobile(tenant_id, mobile) 要求软删后用户名/手机号腾空，
// 才能让同名用户重新创建。mobile 置 NULL（而非 ”）以避开唯一索引的多空值冲突。
func (r userRepo) Delete(ctx context.Context, u *iam.User, operator string) error {
	conn := db(ctx)
	now := time.Now()
	renamed := iam.RenameSoftDeleted(u.Username, u.ID, now)

	res := conn.Model(&model.User{}).Where("id = ?", u.ID).Updates(map[string]any{
		"username":   renamed,
		"mobile":     nil,
		"deleted_at": now,
		"updater":    operator,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除用户失败")
	}
	if err := conn.Where("user_id = ?", u.ID).Delete(&model.UserRole{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "删除用户失败")
	}
	return appendEvents(ctx, u.PullEvents()...)
}

// UsernameExists 用户名是否已占用。
func (r userRepo) UsernameExists(ctx context.Context, username string, excludeID int64) (bool, error) {
	return existsBy(ctx, &model.User{}, "username", username, excludeID, "检查用户名唯一性失败")
}

// MobileExists 手机号是否已占用（空串视为未占用）。
func (r userRepo) MobileExists(ctx context.Context, mobile string, excludeID int64) (bool, error) {
	if mobile == "" {
		return false, nil
	}
	return existsBy(ctx, &model.User{}, "mobile", mobile, excludeID, "检查手机号唯一性失败")
}

// ReplaceRoles 重建用户角色关联。
func (r userRepo) ReplaceRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	return r.replaceRolesTx(ctx, userID, roleIDs)
}

func (r userRepo) replaceRolesTx(ctx context.Context, userID int64, roleIDs []int64) error {
	conn := db(ctx)
	if err := conn.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "清理用户角色失败")
	}
	if len(roleIDs) == 0 {
		return nil
	}
	rows := make([]model.UserRole, 0, len(roleIDs))
	for _, rid := range roleIDs {
		rows = append(rows, model.UserRole{UserID: userID, RoleID: rid})
	}
	if err := conn.Create(&rows).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "保存用户角色失败")
	}
	return nil
}

func (r userRepo) roleIDs(ctx context.Context, userID int64) ([]int64, error) {
	var ids []int64
	if err := db(ctx).Model(&model.UserRole{}).
		Where("user_id = ?", userID).Pluck("role_id", &ids).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户角色失败")
	}
	return ids, nil
}

// RoleCodesByIDs 按角色 ID 查编码（顺带校验归属）。
//
// 安全方向：任一 ID 不存在或非本租户即报错——租户插件保证了非本租户的行查不到，
// 数量比对即等价于归属校验。
func (r userRepo) RoleCodesByIDs(ctx context.Context, roleIDs []int64) ([]string, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var roles []model.Role
	if err := db(ctx).Where("id IN ?", roleIDs).Find(&roles).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色失败")
	}
	if len(roles) != len(roleIDs) {
		return nil, errs.New(errs.CodeParam, "存在无效的角色")
	}
	codes := make([]string, 0, len(roles))
	for _, x := range roles {
		codes = append(codes, x.Code)
	}
	return codes, nil
}

// TouchLogin 记录登录 IP 与时间。
func (r userRepo) TouchLogin(ctx context.Context, userID int64, ip string) error {
	now := time.Now()
	if err := db(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"login_ip":   ip,
		"login_date": &now,
	}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "更新登录信息失败")
	}
	return nil
}

// existsBy 通用唯一性检查。
//
// 改造前该逻辑在 dao 里被复制 5 份（username/mobile/role code/post code/tenant code），
// 现在收敛为唯一实现。
func existsBy(ctx context.Context, m any, column, value string, excludeID int64, failMsg string) (bool, error) {
	conn := db(ctx).Model(m).Where(column+" = ?", value)
	if excludeID > 0 {
		conn = conn.Where("id != ?", excludeID)
	}
	var cnt int64
	if err := conn.Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, failMsg)
	}
	return cnt > 0, nil
}

// toUserPO 领域 → 持久化对象。
func toUserPO(u *iam.User) *model.User {
	return &model.User{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: u.Nickname, Updater: u.Nickname},
			TenantID:  u.TenantID,
		},
		Username:  u.Username,
		Password:  u.Password,
		Nickname:  u.Nickname,
		Remark:    u.Remark,
		DeptID:    u.DeptID,
		PostIDs:   iam.EncodeIDArray(u.PostIDs),
		Email:     u.Email,
		Mobile:    u.Mobile,
		Gender:    u.Gender,
		Avatar:    u.Avatar,
		Status:    int8(u.Status),
		LoginIP:   u.LoginIP,
		LoginDate: u.LoginDate,
	}
}

// toUserDomain 持久化对象 → 领域。
func toUserDomain(po *model.User) *iam.User {
	return &iam.User{
		ID:         po.ID,
		TenantID:   po.TenantID,
		Username:   po.Username,
		Password:   po.Password,
		Nickname:   po.Nickname,
		Remark:     po.Remark,
		DeptID:     po.DeptID,
		PostIDs:    iam.DecodeIDArray(po.PostIDs),
		Email:      po.Email,
		Mobile:     po.Mobile,
		Gender:     po.Gender,
		Avatar:     po.Avatar,
		Status:     iam.UserStatus(po.Status),
		LoginIP:    po.LoginIP,
		LoginDate:  po.LoginDate,
		CreateTime: po.CreateTime,
		UpdateTime: po.UpdateTime,
	}
}

// toUserUpdates 领域 → 更新列映射。
//
// 改造前是 service/user.go:169-189 手写的 map[string]any，键名为魔法字符串；
// NULL/空串特例（post_ids 空存 NULL、mobile 空存 NULL 以避开唯一索引多空冲突）
// 在此收口——存储约束属于仓储职责，不该泄漏到应用层。
func toUserUpdates(u *iam.User) map[string]any {
	upd := map[string]any{
		"username": u.Username,
		"nickname": u.Nickname,
		"remark":   u.Remark,
		"dept_id":  u.DeptID,
		"email":    u.Email,
		"gender":   u.Gender,
		"avatar":   u.Avatar,
		"status":   int8(u.Status),
	}
	// 空集合存 NULL：json 列拒绝 ''，且多 NULL 不触发唯一索引冲突。
	if encoded := iam.EncodeIDArray(u.PostIDs); encoded != "" {
		upd["post_ids"] = encoded
	} else {
		upd["post_ids"] = nil
	}
	if u.Mobile != nil && *u.Mobile != "" {
		upd["mobile"] = *u.Mobile
	} else {
		upd["mobile"] = nil
	}
	return upd
}
