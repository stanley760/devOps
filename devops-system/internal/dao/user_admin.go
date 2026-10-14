// 用户管理 dao（P5）：纯模型查询由 TenantPlugin 注入租户过滤。
package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-common/domain"
	"github.com/stanley760/devops/devops-common/domain/gormx"
	"github.com/stanley760/devops/devops-infra/datascope"
	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-system/internal/model"
	"github.com/stanley760/devops/devops-system/internal/tenantdb"
	"github.com/stanley760/devops/devops-system/internal/types"
)

// ListUsers 分页查询用户（筛选 username 模糊/mobile/状态/部门；行级数据权限收敛）。
// datascope.Apply 注入 ctx 中的可见范围（users 路由挂 HumaDataScope 解析中间件）；
// 未注入时 fail-closed 查询报错，防漏配退化为全量可见。
func ListUsers(ctx context.Context, q types.UserPageQuery) (domain.PageResult[model.User], error) {
	db := datascope.Apply(ctx, tenantdb.DB(ctx).Model(&model.User{}),
		datascope.Columns{Dept: "dept_id", Owner: "id"})
	if q.Username != "" {
		db = db.Where("username LIKE ?", "%"+q.Username+"%")
	}
	if q.Mobile != "" {
		db = db.Where("mobile = ?", q.Mobile)
	}
	if q.Status != nil {
		db = db.Where("status = ?", *q.Status)
	}
	if q.DeptID != nil {
		db = db.Where("dept_id = ?", q.DeptID.Int64())
	}

	page, err := gormx.FindPage[model.User](
		db.Order("id DESC"),
		domain.PageParam{PageNum: q.PageNum, PageSize: q.PageSize},
	)
	if err != nil {
		return domain.PageResult[model.User]{}, errs.Wrap(err, errs.CodeInternal, "查询用户列表失败")
	}
	return page, nil
}

// UsernameExists 租户内用户名是否已占用（excludeID 排除自身，更新场景）。
func UsernameExists(ctx context.Context, username string, excludeID int64) (bool, error) {
	db := tenantdb.DB(ctx).Model(&model.User{}).Where("username = ?", username)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	var cnt int64
	if err := db.Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "检查用户名唯一性失败")
	}
	return cnt > 0, nil
}

// MobileExists 租户内手机号是否已占用。
func MobileExists(ctx context.Context, mobile string, excludeID int64) (bool, error) {
	if mobile == "" {
		return false, nil
	}
	db := tenantdb.DB(ctx).Model(&model.User{}).Where("mobile = ?", mobile)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	var cnt int64
	if err := db.Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "检查手机号唯一性失败")
	}
	return cnt > 0, nil
}

// CreateUser 创建用户（主键经 BeforeCreate 发号）。
func CreateUser(ctx context.Context, u *model.User) error {
	if err := tenantdb.DB(ctx).Create(u).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "创建用户失败")
	}
	return nil
}

// UpdateUser 更新用户可变字段（map 形式，仅非零字段）。
func UpdateUser(ctx context.Context, userID int64, updates map[string]any) error {
	res := tenantdb.DB(ctx).Model(&model.User{}).
		Where("id = ?", userID).Updates(updates)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新用户失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "用户不存在")
	}
	return nil
}

// SoftDeleteUserTx 软删用户（事务内）：改写 username/mobile 绕开租户内唯一索引，
// 保证同 username 可重新创建（方案「软删同名重建」）。
func SoftDeleteUserTx(tx *gorm.DB, userID int64, u *model.User, operator string) error {
	now := time.Now()
	res := tx.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"username":   fmt.Sprintf("%s_del_%d_%d", u.Username, userID, now.Unix()),
		"mobile":     nil,
		"deleted_at": now,
		"updater":    operator,
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除用户失败")
	}
	return nil
}

// SyncUserRolesTx 事务内重建用户-角色关联（清空再批插）。
func SyncUserRolesTx(tx *gorm.DB, userID int64, roleIDs []int64) error {
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "清理用户角色失败")
	}
	if len(roleIDs) == 0 {
		return nil
	}
	rows := make([]model.UserRole, 0, len(roleIDs))
	for _, rid := range roleIDs {
		rows = append(rows, model.UserRole{UserID: userID, RoleID: rid})
	}
	return tx.Create(&rows).Error
}

// CreateUserWithRoles 建用户 + 初始角色关联（单事务）。
func CreateUserWithRoles(ctx context.Context, u *model.User, roleIDs []int64) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建用户失败")
		}
		if len(roleIDs) > 0 {
			rows := make([]model.UserRole, 0, len(roleIDs))
			for _, rid := range roleIDs {
				rows = append(rows, model.UserRole{UserID: u.ID, RoleID: rid})
			}
			if err := tx.Create(&rows).Error; err != nil {
				return errs.Wrap(err, errs.CodeInternal, "保存用户角色失败")
			}
		}
		return nil
	})
}

// UpdateUserWithRoles 更新用户字段 + 重建角色关联（单事务）。
func UpdateUserWithRoles(ctx context.Context, userID int64, updates map[string]any, roleIDs []int64) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "更新用户失败")
		}
		return SyncUserRolesTx(tx, userID, roleIDs)
	})
}

// DeleteUser 软删用户 + 清角色关联（单事务）。
func DeleteUser(ctx context.Context, u *model.User, operator string) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		if err := SoftDeleteUserTx(tx, u.ID, u, operator); err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.ID).Delete(&model.UserRole{}).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "删除用户失败")
		}
		return nil
	})
}

// GetUserRoleIDs 用户在租户内的角色 ID 列表。
func GetUserRoleIDs(ctx context.Context, userID int64) ([]int64, error) {
	var ids []int64
	err := tenantdb.DB(ctx).Model(&model.UserRole{}).
		Where("user_id = ?", userID).Pluck("role_id", &ids).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询用户角色失败")
	}
	return ids, nil
}

// RoleCodesByIDs 按角色 ID 集合查角色编码（租户内；顺带校验归属）。
// 返回按入参顺序对应的编码；任一 ID 不存在/非本租户返回错误。
func RoleCodesByIDs(ctx context.Context, roleIDs []int64) ([]string, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var roles []model.Role
	err := tenantdb.DB(ctx).
		Where("id IN ?", roleIDs).Find(&roles).Error
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询角色失败")
	}
	if len(roles) != len(roleIDs) {
		return nil, errs.New(errs.CodeParam, "存在无效的角色")
	}
	codes := make([]string, 0, len(roles))
	for _, r := range roles {
		codes = append(codes, r.Code)
	}
	return codes, nil
}

// PostIDsOf 用户 post_ids JSON 字符串转 []ID。存储唯一形态是数字数组（PostIDsJSON
// 写侧收口）；字符串元素（["4"]）视为脏数据，整体解析失败返回空集，不做兼容读。
func PostIDsOf(postIDs string) []types.ID {
	if postIDs == "" {
		return []types.ID{}
	}
	var ids []int64
	if err := json.Unmarshal([]byte(postIDs), &ids); err != nil {
		return []types.ID{}
	}
	out := make([]types.ID, 0, len(ids))
	for _, id := range ids {
		out = append(out, types.ID(id))
	}
	return out
}

// PostIDsJSON []ID → JSON 字符串（元素转 int64 后 marshal，落库形态为数字数组 [4,5]；
// 空集返回 ""，由调用方决定写 NULL）。
// 不直接 marshal []types.ID——其 MarshalJSON 输出字符串元素（["4","5"]，API 线上契约），
// 会泄漏进存储层，与种子数据及 JSON_CONTAINS(post_ids, 4) 的数字匹配口径不一致。
func PostIDsJSON(ids []types.ID) string {
	if len(ids) == 0 {
		return ""
	}
	i64s := make([]int64, 0, len(ids))
	for _, id := range ids {
		i64s = append(i64s, id.Int64())
	}
	b, err := json.Marshal(i64s)
	if err != nil {
		return ""
	}
	return string(b)
}
