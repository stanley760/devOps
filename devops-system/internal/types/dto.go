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
	Mobile    string    `json:"mobile" doc:"手机号码"`
	Gender    int8      `json:"gender" doc:"性别 0未知 1男 2女"`
	Avatar    string    `json:"avatar" doc:"头像"`
	DeptID    ID        `json:"deptId" doc:"部门id"`
	PostIDs   []ID      `json:"postIds" doc:"关联岗位id列表"`
	Remark    string    `json:"remark" doc:"备注"`
	LoginIP   string    `json:"loginIp" doc:"登录ip地址"`
	LoginDate time.Time `json:"loginDate" doc:"登录时间"`
}

// UserInfoResp GET /auth/info 响应。
type UserInfoResp struct {
	User        UserDTO     `json:"user" doc:"用户信息"`
	Tenant      TenantBrief `json:"tenant" doc:"租户"`
	Roles       []string    `json:"roles" doc:"角色列表"`       // 角色编码
	Permissions []string    `json:"permissions" doc:"权限列表"` // 权限标识
}

// RouterNode 前端路由节点（菜单树）。
type RouterNode struct {
	ID            ID           `json:"id" doc:"菜单id"`
	Name          string       `json:"name" doc:"菜单名称"`
	Path          string       `json:"path" doc:"菜单路径"`
	Component     string       `json:"component" doc:"组件路径"`
	ComponentName string       `json:"componentName" doc:"组件名称"`
	Icon          string       `json:"icon" doc:"图标"`
	Sort          int          `json:"sort" doc:"排序"`
	Visible       bool         `json:"visible" doc:"是否可见"`
	KeepAlive     bool         `json:"keepAlive" doc:"是否缓存"`
	AlwaysShow    bool         `json:"alwaysShow" doc:"是否总是显示"`
	Children      []RouterNode `json:"children" doc:"子路由"`
}

// UpdateProfileReq PUT /auth/profile 入参。
type UpdateProfileReq struct {
	Nickname string `json:"nickname" binding:"required,min=1,max=30" minLength:"1" maxLength:"30" doc:"昵称"`
	Email    string `json:"email,omitempty" binding:"omitempty,email,max=50" format:"email" maxLength:"50" doc:"邮箱账号"`
	Mobile   string `json:"mobile,omitempty" binding:"omitempty,max=20" maxLength:"20" doc:"手机号码"`
	Gender   int8   `json:"gender,omitempty" binding:"omitempty,min=0,max=2" minimum:"0" maximum:"2" doc:"性别 0未知 1男 2女"`
	Avatar   string `json:"avatar,omitempty" binding:"omitempty,max=512" maxLength:"512" doc:"图片链接"`
}

// UpdatePasswordReq PUT /auth/profile/password 入参。
type UpdatePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required,min=6,max=64" minLength:"6" maxLength:"64" doc:"旧密码"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=64" minLength:"6" maxLength:"64" doc:"新密码"`
}
