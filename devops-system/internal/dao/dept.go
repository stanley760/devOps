// 部门/岗位管理 dao。
package dao

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-common/domain"
	"github.com/stanley760/devOps/devops-infra/errs"
	"github.com/stanley760/devOps/devops-system/internal/model"
	"github.com/stanley760/devOps/devops-system/internal/tenantdb"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// ---- 部门 ----

// ListDepts 全量部门（树由 service 内存组装）。
func ListDepts(ctx context.Context, status *int8) ([]model.Dept, error) {
	db := tenantdb.DB(ctx).Model(&model.Dept{})
	if status != nil {
		db = db.Where("status = ?", *status)
	}
	var depts []model.Dept
	if err := db.Order("sort ASC, id ASC").Find(&depts).Error; err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "查询部门失败")
	}
	return depts, nil
}

// GetDeptByID 按 ID 取部门（租户内）。
func GetDeptByID(ctx context.Context, deptID int64) (*model.Dept, error) {
	var d model.Dept
	err := tenantdb.DB(ctx).Where("id = ?", deptID).First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "部门不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询部门失败")
	}
	return &d, nil
}

// CreateDept 创建部门（主键发号）。
func CreateDept(ctx context.Context, d *model.Dept) error {
	if err := tenantdb.DB(ctx).Create(d).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "创建部门失败")
	}
	return nil
}

// UpdateDept 更新部门字段。
func UpdateDept(ctx context.Context, deptID int64, updates map[string]any) error {
	res := tenantdb.DB(ctx).Model(&model.Dept{}).
		Where("id = ?", deptID).Updates(updates)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新部门失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "部门不存在")
	}
	return nil
}

// DeptHasChildren 是否存在未删除子部门。
func DeptHasChildren(ctx context.Context, deptID int64) (bool, error) {
	var cnt int64
	err := tenantdb.DB(ctx).Model(&model.Dept{}).
		Where("parent_id = ?", deptID).Count(&cnt).Error
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询子部门失败")
	}
	return cnt > 0, nil
}

// DeptHasUsers 部门下是否有用户。
func DeptHasUsers(ctx context.Context, deptID int64) (bool, error) {
	var cnt int64
	err := tenantdb.DB(ctx).Model(&model.User{}).
		Where("dept_id = ?", deptID).Count(&cnt).Error
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询部门用户失败")
	}
	return cnt > 0, nil
}

// DeleteDept 软删部门（调用方保证无子部门且无用户）。
func DeleteDept(ctx context.Context, deptID int64) error {
	res := tenantdb.DB(ctx).Delete(&model.Dept{}, deptID)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除部门失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "部门不存在")
	}
	return nil
}

// ---- 岗位 ----

// ListPosts 分页查询岗位（筛选 code/name/status）。
func ListPosts(ctx context.Context, q types.PostPageQuery) (domain.PageResult[model.Post], error) {
	db := tenantdb.DB(ctx).Model(&model.Post{})
	if q.Code != "" {
		db = db.Where("code = ?", q.Code)
	}
	if q.Name != "" {
		db = db.Where("name LIKE ?", "%"+q.Name+"%")
	}
	if q.Status != nil {
		db = db.Where("status = ?", *q.Status)
	}

	page, err := domain.FindPage[model.Post](
		db.Order("sort ASC, id ASC"),
		domain.PageParam{PageNum: q.PageNum, PageSize: q.PageSize},
	)
	if err != nil {
		return domain.PageResult[model.Post]{}, errs.Wrap(err, errs.CodeInternal, "查询岗位列表失败")
	}
	return page, nil
}

// GetPostByID 按 ID 取岗位（租户内）。
func GetPostByID(ctx context.Context, postID int64) (*model.Post, error) {
	var p model.Post
	err := tenantdb.DB(ctx).Where("id = ?", postID).First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "岗位不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询岗位失败")
	}
	return &p, nil
}

// PostCodeExists 租户内岗位编码是否已占用。
func PostCodeExists(ctx context.Context, code string, excludeID int64) (bool, error) {
	db := tenantdb.DB(ctx).Model(&model.Post{}).Where("code = ?", code)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	var cnt int64
	if err := db.Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "检查岗位编码唯一性失败")
	}
	return cnt > 0, nil
}

// CreatePost 创建岗位（主键发号）。
func CreatePost(ctx context.Context, p *model.Post) error {
	if err := tenantdb.DB(ctx).Create(p).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "创建岗位失败")
	}
	return nil
}

// UpdatePost 更新岗位字段。
func UpdatePost(ctx context.Context, postID int64, updates map[string]any) error {
	res := tenantdb.DB(ctx).Model(&model.Post{}).
		Where("id = ?", postID).Updates(updates)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新岗位失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "岗位不存在")
	}
	return nil
}

// PostReferencedByUsers 岗位是否被用户 post_ids 引用（JSON_CONTAINS）。
func PostReferencedByUsers(ctx context.Context, postID int64) (bool, error) {
	var cnt int64
	err := tenantdb.DB(ctx).Model(&model.User{}).
		Where("JSON_CONTAINS(post_ids, ?)", postID).Count(&cnt).Error
	if err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "查询岗位引用失败")
	}
	return cnt > 0, nil
}

// SoftDeletePostTx 事务内软删岗位：改写 code 绕开租户内唯一索引（同名可重建）。
func SoftDeletePostTx(tx *gorm.DB, postID int64, code string) error {
	res := tx.Model(&model.Post{}).Where("id = ?", postID).Updates(map[string]any{
		"code":       code + "_del_" + types.ID(postID).String(),
		"deleted_at": time.Now(),
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除岗位失败")
	}
	return nil
}

// SoftDeletePost 软删岗位（单事务，见 SoftDeletePostTx）。
func SoftDeletePost(ctx context.Context, postID int64, code string) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		return SoftDeletePostTx(tx, postID, code)
	})
}
