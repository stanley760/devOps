// 认证与用户信息 DTO。ID 一律 types.ID 字符串形态下发。
package types

import "time"

// LoginReq 登录入参。tenant_code 为空取 tenant.default_code。
type LoginReq struct {
	TenantCode string `json:"tenant_code" binding:"omitempty,max=30"`
	Username   string `json:"username" binding:"required,min=2,max=30"`
	Password   string `json:"password" binding:"required,min=6,max=64"`
}

// TenantBrief 租户摘要。
type TenantBrief struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// LoginResp 登录响应：双 token + 租户。
type LoginResp struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	ExpiresIn    int64       `json:"expires_in"` // access token 有效期（秒）
	Tenant       TenantBrief `json:"tenant"`
}

// RefreshReq 刷新入参。
type RefreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required,uuid"`
}

// TokenResp 刷新响应（结构同 LoginResp）。
type TokenResp = LoginResp

// UserDTO 用户信息。
type UserDTO struct {
	ID        ID        `json:"id"`
	Username  string    `json:"username"`
	Nickname  string    `json:"nickname"`
	Email     string    `json:"email"`
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
	Nickname string `json:"nickname" binding:"required,min=1,max=30"`
	Email    string `json:"email" binding:"omitempty,email,max=50"`
	Mobile   string `json:"mobile" binding:"omitempty,max=20"`
	Sex      int8   `json:"sex" binding:"omitempty,min=0,max=2"`
	Avatar   string `json:"avatar" binding:"omitempty,max=512"`
}

// UpdatePasswordReq PUT /auth/profile/password 入参。
type UpdatePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required,min=6,max=64"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=64"`
}
