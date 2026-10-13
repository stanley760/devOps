// 认证与用户信息 DTO。ID 一律 types.ID 字符串形态下发。
package types

import "time"

// LoginReq 登录入参。tenant_code 为空取 tenant.default_code。
type LoginReq struct {
	TenantCode string `json:"tenant_code,omitempty" binding:"omitempty,max=30" maxLength:"30" doc:"租户编号"`
	Username   string `json:"username" binding:"required,min=2,max=30" minLength:"2" maxLength:"30" doc:"用户姓名"`
	Password   string `json:"password" binding:"required,min=6,max=64" minLength:"6" maxLength:"64" doc:"用户密码"`
}

// TenantBrief 租户摘要。
type TenantBrief struct {
	ID   ID     `json:"id" doc:"租户id"`
	Name string `json:"name" doc:"租户名字"`
	Code string `json:"code" doc:"租户编码"`
}

// LoginResp 登录响应：双 token + 租户。
type LoginResp struct {
	AccessToken  string      `json:"access_token" doc:"当前token"`
	RefreshToken string      `json:"refresh_token" doc:"刷新token"`
	ExpiresIn    int64       `json:"expires_in" doc:"当前token有效期"` // access token 有效期（秒）
	Tenant       TenantBrief `json:"tenant" doc:"租户信息"`
}

// RefreshReq 刷新入参。
type RefreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required,uuid" format:"uuid" doc:"刷新token"`
}

// TokenResp 刷新响应（结构同 LoginResp）。
type TokenResp = LoginResp

// UserDTO 用户信息。
type UserDTO struct {
	ID        ID        `json:"id" doc:"用户id"`
	Username  string    `json:"username" doc:"用户名称"`
	Nickname  string    `json:"nickname" doc:"用户昵称"`
	Email     string    `json:"email" doc:"电子邮箱"`
	Mobile    string    `json:"mobile"`
	Sex       int8      `json:"sex"`
	Avatar    string    `json:"avatar"`
	DeptID    ID        `json:"deptId"`
	PostIDs   []ID      `json:"postIds"`
	Remark    string    `json:"remark"`
	LoginIP   string    `json:"loginIp"`
	LoginDate time.Time `json:"loginDate"`
}

// UserInfoResp GET /auth/info 响应。
type UserInfoResp struct {
	User        UserDTO     `json:"user"`
	Tenant      TenantBrief `json:"tenant"`
	Roles       []string    `json:"roles"`       // 角色编码
	Permissions []string    `json:"permissions"` // 权限标识
}

// RouterNode 前端路由节点（菜单树 type 1/2 转换）。
type RouterNode struct {
	ID            ID           `json:"id"`
	Name          string       `json:"name"`
	Path          string       `json:"path"`
	Component     string       `json:"component"`
	ComponentName string       `json:"componentName"`
	Icon          string       `json:"icon"`
	Sort          int          `json:"sort"`
	Visible       bool         `json:"visible"`
	KeepAlive     bool         `json:"keepAlive"`
	AlwaysShow    bool         `json:"alwaysShow"`
	Children      []RouterNode `json:"children"`
}

// UpdateProfileReq PUT /auth/profile 入参。
type UpdateProfileReq struct {
	Nickname string `json:"nickname" binding:"required,min=1,max=30" minLength:"1" maxLength:"30"`
	Email    string `json:"email,omitempty" binding:"omitempty,email,max=50" format:"email" maxLength:"50"`
	Mobile   string `json:"mobile,omitempty" binding:"omitempty,max=20" maxLength:"20"`
	Sex      int8   `json:"sex,omitempty" binding:"omitempty,min=0,max=2" minimum:"0" maximum:"2"`
	Avatar   string `json:"avatar,omitempty" binding:"omitempty,max=512" maxLength:"512"`
}

// UpdatePasswordReq PUT /auth/profile/password 入参。
type UpdatePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required,min=6,max=64" minLength:"6" maxLength:"64"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=64" minLength:"6" maxLength:"64"`
}
