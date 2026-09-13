// 租户管理模块 DTO。
package types

import "time"

// TenantPageQuery GET /tenants 查询参数。
type TenantPageQuery struct {
	PageNum  int    `form:"page_num" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Name     string `form:"name" binding:"omitempty,max=30"`
	Code     string `form:"code" binding:"omitempty,max=30"`
	Status   *int8  `form:"status" binding:"omitempty,min=0,max=1"`
}

// TenantCreateReq POST /tenants 入参（含租户初始超管，创建后即可登录）。
type TenantCreateReq struct {
	Name          string     `json:"name" binding:"required,min=1,max=30"`
	Code          string     `json:"code" binding:"required,min=2,max=30"`
	ContactName   string     `json:"contact_name" binding:"omitempty,max=30"`
	ContactMobile string     `json:"contact_mobile" binding:"omitempty,max=20"`
	ExpireTime    *time.Time `json:"expire_time"`
	Remark        string     `json:"remark" binding:"omitempty,max=500"`
	AdminUsername string     `json:"admin_username" binding:"required,min=2,max=30"`
	AdminPassword string     `json:"admin_password" binding:"required,min=6,max=32"`
}

// TenantUpdateReq PUT /tenants/:id 入参（code 不可改）。
type TenantUpdateReq struct {
	Name          string     `json:"name" binding:"required,min=1,max=30"`
	ContactName   string     `json:"contact_name" binding:"omitempty,max=30"`
	ContactMobile string     `json:"contact_mobile" binding:"omitempty,max=20"`
	ExpireTime    *time.Time `json:"expire_time"` // NULL 永不过期（显式传 null 清除）
	Remark        string     `json:"remark" binding:"omitempty,max=500"`
}

// TenantStatusReq PUT /tenants/:id/status 入参。
type TenantStatusReq struct {
	Status int8 `json:"status" binding:"omitempty,min=0,max=1"` // 0启用 1停用（required 会拒 0 值，故不用）
}

// TenantDTO 租户信息。
type TenantDTO struct {
	ID            ID         `json:"id"`
	Name          string     `json:"name"`
	Code          string     `json:"code"`
	ContactName   string     `json:"contactName"`
	ContactMobile string     `json:"contactMobile"`
	Status        int8       `json:"status"`
	ExpireTime    *time.Time `json:"expireTime"`
	Remark        string     `json:"remark"`
	CreateTime    time.Time  `json:"createTime"`
}
