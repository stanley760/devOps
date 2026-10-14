// 租户管理模块 DTO。
package types

import (
	"time"

	"github.com/stanley760/devops/devops-common/domain"
)

// TenantPageQuery GET /tenants 查询参数。
type TenantPageQuery struct {
	domain.PageParam
	Name   string `form:"name" binding:"omitempty,max=30" doc:"租户名称"`
	Code   string `form:"code" binding:"omitempty,max=30" doc:"租户编码"`
	Status *int8  `form:"status" binding:"omitempty,min=0,max=1" doc:"租户状态 0:停用；1:启用"`
}

// TenantCreateReq POST /tenants 入参（含租户初始超管，创建后即可登录）。
type TenantCreateReq struct {
	Name          string     `json:"name" binding:"required,min=1,max=30" minLength:"1" maxLength:"30" doc:"租户名称"`
	Code          string     `json:"code" binding:"required,min=2,max=30" minLength:"2" maxLength:"30" doc:"租户编码，创建后不可修改"`
	ContactName   string     `json:"contact_name,omitempty" binding:"omitempty,max=30" maxLength:"30" doc:"联系人姓名"`
	ContactMobile string     `json:"contact_mobile,omitempty" binding:"omitempty,max=20" maxLength:"20" doc:"联系人手机号"`
	ExpireTime    *time.Time `json:"expire_time,omitempty" doc:"过期时间，不传为永不过期"`
	Remark        string     `json:"remark,omitempty" binding:"omitempty,max=500" maxLength:"500" doc:"备注"`
	AdminUsername string     `json:"admin_username" binding:"required,min=2,max=30" minLength:"2" maxLength:"30" doc:"租户初始超管用户名"`
	AdminPassword string     `json:"admin_password" binding:"required,min=6,max=32" minLength:"6" maxLength:"32" doc:"租户初始超管密码"`
}

// TenantUpdateReq PUT /tenants/:id 入参（code 不可改）。
type TenantUpdateReq struct {
	Name          string     `json:"name" binding:"required,min=1,max=30" minLength:"1" maxLength:"30" doc:"租户名称"`
	ContactName   string     `json:"contact_name,omitempty" binding:"omitempty,max=30" maxLength:"30" doc:"联系人姓名"`
	ContactMobile string     `json:"contact_mobile,omitempty" binding:"omitempty,max=20" maxLength:"20" doc:"联系人手机号"`
	ExpireTime    *time.Time `json:"expire_time,omitempty" doc:"过期时间，显式传 null 清除（永不过期）"`
	Remark        string     `json:"remark,omitempty" binding:"omitempty,max=500" maxLength:"500" doc:"备注"`
}

// TenantStatusReq PUT /tenants/:id/status 入参。
type TenantStatusReq struct {
	Status int8 `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1" doc:"租户状态 0:停用；1:启用"` // required 会拒 0 值，故不用
}

// TenantDTO 租户信息。
type TenantDTO struct {
	ID            ID         `json:"id" doc:"租户id"`
	Name          string     `json:"name" doc:"租户名称"`
	Code          string     `json:"code" doc:"租户编码"`
	ContactName   string     `json:"contactName" doc:"联系人姓名"`
	ContactMobile string     `json:"contactMobile" doc:"联系人手机号"`
	Status        int8       `json:"status" doc:"租户状态 0:停用；1:启用"`
	ExpireTime    *time.Time `json:"expireTime" doc:"过期时间，为空表示永不过期"`
	Remark        string     `json:"remark" doc:"备注"`
	CreateTime    time.Time  `json:"createTime" doc:"创建日期"`
}
