// Package domain 提供跨业务复用的分页值对象与元数据。
//
// 本包保持零外部依赖（仅标准库），使不使用 GORM 的业务模块也能直接复用分页契约；
// 依赖 GORM 的查询辅助位于子包 gormx。
package domain

// PageParam 分页入参（HTTP query/form 绑定）。
type PageParam struct {
	PageNum  int `query:"page_num" form:"page_num" binding:"omitempty,min=1" minimum:"1" default:"1" doc:"页码"`
	PageSize int `query:"page_size" form:"page_size" binding:"omitempty,min=1,max=100" minimum:"1" maximum:"100" default:"10" doc:"每页记录数"`
}

const (
	DefaultPageNum  = 1
	DefaultPageSize = 10
	MaxPageSize     = 1000
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