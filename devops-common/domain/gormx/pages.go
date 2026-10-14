// Package gormx 提供基于 GORM 的分页查询辅助。
//
// 与父包 domain 分离，使 domain 保持零外部依赖：分页值对象是跨业务契约，
// 而 GORM 查询辅助只服务于使用 GORM 的存储实现。
package gormx

import (
	"gorm.io/gorm"

	"github.com/stanley760/devops/devops-common/domain"
)

// FindPage 统计符合条件的记录数并查询当前页。
// 调用前在 db 上添加 Model、Where 等筛选条件和 Order 排序，不要预先设置 Limit/Offset。
func FindPage[T any](db *gorm.DB, param domain.PageParam) (domain.PageResult[T], error) {
	param = param.Normalize()
	result := domain.PageResult[T]{
		List:     make([]T, 0),
		PageNum:  param.PageNum,
		PageSize: param.PageSize,
	}

	if err := db.Session(&gorm.Session{}).Count(&result.Total).Error; err != nil {
		return result, err
	}
	if err := db.Session(&gorm.Session{}).
		Offset(param.Offset()).
		Limit(param.PageSize).
		Find(&result.List).Error; err != nil {
		return result, err
	}
	if result.Total > 0 {
		result.Pages = (result.Total-1)/int64(param.PageSize) + 1
	}
	return result, nil
}

// Paginate 返回可直接挂到 GORM 链上的 Offset/Limit 片段。
func Paginate(page, pageSize int) func(*gorm.DB) *gorm.DB {
	param := (domain.PageParam{PageNum: page, PageSize: pageSize}).Normalize()
	return func(db *gorm.DB) *gorm.DB {
		return db.Offset(param.Offset()).Limit(param.PageSize)
	}
}