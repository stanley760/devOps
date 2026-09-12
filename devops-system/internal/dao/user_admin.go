// 用户管理 dao（P5）：纯模型查询由 TenantPlugin 注入租户过滤。
package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"devops-infra/errs"

	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
	"devops-system/internal/types"
)

// ListUsers 分页查询用户（筛选 username 模糊/mobile/状态/部门）。
func ListUsers(ctx context.Context, q types.UserPageQuery) ([]model.User, int64, error) {
	db := tenantdb.DB(ctx).Model(&model.User{})
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

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "查询用户总数失败")
	}

	pageNum, pageSize := q.PageNum, q.PageSize
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	var users []model.User
	if err := db.Order("id DESC").
		Offset((pageNum - 1) * pageSize).Limit(pageSize).
		Find(&users).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "查询用户列表失败")
	}
	return users, total, nil
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

// PostIDsOf 用户 post_ids JSON 字符串转 []ID。
func PostIDsOf(postIDs string) []types.ID {
	if postIDs == "" {
		return []types.ID{}
	}
	var ids []types.ID
	if err := json.Unmarshal([]byte(postIDs), &ids); err != nil {
		return []types.ID{}
	}
	return ids
}

// PostIDsJSON []ID → JSON 字符串（空集返回 ""，由调用方决定写 NULL）。
func PostIDsJSON(ids []types.ID) string {
	if len(ids) == 0 {
		return ""
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(b)
}
