package domain

import "gorm.io/gorm"

type PageParam struct {
	PageNum  int `query:"page_num" form:"page_num" binding:"omitempty,min=1" minimum:"1" default:"1" doc:"页码"`
	PageSize int `query:"page_size" form:"page_size" binding:"omitempty,min=1,max=100" minimum:"1" maximum:"100" default:"10" doc:"每页记录数"`
}

const (
	DefaultPageNum  = 1
	DefaultPageSize = 10
	MaxPageSize     = 100
)

// Normalize 补齐默认值并限制单页最大条数。
func (p PageParam) Normalize() PageParam {
	if p.PageNum < 1 {
		p.PageNum = DefaultPageNum
	}
	if p.PageSize < 1 {
		p.PageSize = DefaultPageSize
	}
	if p.PageSize > MaxPageSize {
		p.PageSize = MaxPageSize
	}
	return p
}

// Offset 返回当前页对应的数据库偏移量。
func (p PageParam) Offset() int {
	p = p.Normalize()
	maxInt := int(^uint(0) >> 1)
	if p.PageNum-1 > maxInt/p.PageSize {
		return maxInt
	}
	return (p.PageNum - 1) * p.PageSize
}

// PageResult 是带分页元数据的查询结果。
type PageResult[T any] struct {
	List     []T   `json:"list" doc:"当前页数据列表" required:"true"`
	Total    int64 `json:"total" doc:"符合查询条件的总记录数" required:"true"`
	PageNum  int   `json:"page_num" doc:"当前页码，从 1 开始" required:"true"`
	PageSize int   `json:"page_size" doc:"每页记录数" required:"true"`
	Pages    int64 `json:"pages" doc:"总页数" required:"true"`
}

// MapPage 转换当前页的数据，同时保留分页元数据。
func MapPage[T, U any](page PageResult[T], convert func(T) U) PageResult[U] {
	mapped := PageResult[U]{
		List:     make([]U, 0, len(page.List)),
		Total:    page.Total,
		PageNum:  page.PageNum,
		PageSize: page.PageSize,
		Pages:    page.Pages,
	}
	for _, item := range page.List {
		mapped.List = append(mapped.List, convert(item))
	}
	return mapped
}

// FindPage 统计符合条件的记录数并查询当前页。
// 调用前在 db 上添加 Model、Where 等筛选条件和 Order 排序，不要预先设置 Limit/Offset。
func FindPage[T any](db *gorm.DB, param PageParam) (PageResult[T], error) {
	param = param.Normalize()
	result := PageResult[T]{
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

func Paginate(page, pageSize int) func(*gorm.DB) *gorm.DB {
	param := (PageParam{PageNum: page, PageSize: pageSize}).Normalize()
	return func(db *gorm.DB) *gorm.DB {
		return db.Offset(param.Offset()).Limit(param.PageSize)
	}
}