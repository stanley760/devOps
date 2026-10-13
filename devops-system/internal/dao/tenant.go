// 租户管理 dao（P7）：t_tenant 为全局表（无 tenant_id，TenantPlugin 放行），
// 平台路由 ctx 无租户亦可直接访问。
package dao

import (
	"context"
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/stanley760/devOps/devops-infra/errs"

	"github.com/stanley760/devOps/devops-system/internal/model"
	"github.com/stanley760/devOps/devops-system/internal/tenantdb"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// ListTenants 分页查询租户（筛选 name/code/status）。
func ListTenants(ctx context.Context, q types.TenantPageQuery) ([]model.Tenant, int64, error) {
	db := tenantdb.DB(ctx).Model(&model.Tenant{})
	if q.Name != "" {
		db = db.Where("name LIKE ?", "%"+q.Name+"%")
	}
	if q.Code != "" {
		db = db.Where("code = ?", q.Code)
	}
	if q.Status != nil {
		db = db.Where("status = ?", *q.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "查询租户总数失败")
	}
	pageNum, pageSize := pageOf(q.PageNum, q.PageSize)
	var tenants []model.Tenant
	if err := db.Order("id ASC").
		Offset((pageNum - 1) * pageSize).Limit(pageSize).
		Find(&tenants).Error; err != nil {
		return nil, 0, errs.Wrap(err, errs.CodeInternal, "查询租户列表失败")
	}
	return tenants, total, nil
}

// GetTenantByCode 按编码取租户（登录用：未命中按 401「租户不存在」语义）。
func GetTenantByCode(ctx context.Context, code string) (*model.Tenant, error) {
	var t model.Tenant
	err := tenantdb.DB(ctx).Where("code = ?", code).First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeUnauthorized, "租户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询租户失败")
	}
	return &t, nil
}

// GetTenantByID 按 ID 取租户。
func GetTenantByID(ctx context.Context, tenantID int64) (*model.Tenant, error) {
	var t model.Tenant
	err := tenantdb.DB(ctx).Where("id = ?", tenantID).First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "租户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询租户失败")
	}
	return &t, nil
}

// TenantCodeExists 租户编码是否已占用（全局唯一）。
func TenantCodeExists(ctx context.Context, code string, excludeID int64) (bool, error) {
	db := tenantdb.DB(ctx).Model(&model.Tenant{}).Where("code = ?", code)
	if excludeID > 0 {
		db = db.Where("id != ?", excludeID)
	}
	var cnt int64
	if err := db.Count(&cnt).Error; err != nil {
		return false, errs.Wrap(err, errs.CodeInternal, "检查租户编码唯一性失败")
	}
	return cnt > 0, nil
}

// CreateTenant 创建租户（主键发号，biz_tag=tenant）。
func CreateTenant(ctx context.Context, t *model.Tenant) error {
	if err := tenantdb.DB(ctx).Create(t).Error; err != nil {
		return errs.Wrap(err, errs.CodeInternal, "创建租户失败")
	}
	return nil
}

// UpdateTenant 更新租户字段。
func UpdateTenant(ctx context.Context, tenantID int64, updates map[string]any) error {
	res := tenantdb.DB(ctx).Model(&model.Tenant{}).
		Where("id = ?", tenantID).Updates(updates)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新租户失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "租户不存在")
	}
	return nil
}

// SoftDeleteTenantTx 事务内软删租户：改写 code 绕开全局唯一索引（同 code 可重建）。
func SoftDeleteTenantTx(tx *gorm.DB, tenantID int64, code string) error {
	res := tx.Model(&model.Tenant{}).Where("id = ?", tenantID).Updates(map[string]any{
		"code":       code + "_del_" + strconv.FormatInt(tenantID, 10),
		"deleted_at": time.Now(),
	})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "删除租户失败")
	}
	return nil
}

// SoftDeleteTenant 软删租户（单事务，见 SoftDeleteTenantTx）。
func SoftDeleteTenant(ctx context.Context, tenantID int64, code string) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		return SoftDeleteTenantTx(tx, tenantID, code)
	})
}

// HardDeleteTenant 物理删除租户行：创建流程补偿回滚用。
func HardDeleteTenant(ctx context.Context, tenantID int64) error {
	return tenantdb.DB(ctx).Unscoped().Delete(&model.Tenant{}, tenantID).Error
}

// SeedTenantAdmin 建租户初始超管（super_admin 角色 + 用户 + 关联，单事务）。
// ctx 须为新租户 ctx，用于填充 tenant_id。
func SeedTenantAdmin(ctx context.Context, role *model.Role, admin *model.User) error {
	return WithTx(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(role).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建租户超管角色失败")
		}
		if err := tx.Create(admin).Error; err != nil {
			return errs.Wrap(err, errs.CodeInternal, "创建租户超管用户失败")
		}
		return tx.Create(&model.UserRole{UserID: admin.ID, RoleID: role.ID}).Error
	})
}
