// 用户管理模块 DTO。ID 一律 types.ID 字符串形态。
package types

import "github.com/stanley760/devops/devops-common/domain"

// UserPageQuery GET /users 查询参数。
type UserPageQuery struct {
	domain.PageParam
	Username string `form:"username" binding:"omitempty,max=30" doc:"用户姓名"`
	Mobile   string `form:"mobile" binding:"omitempty,max=20" doc:"手机号码"`
	Status   *int8  `form:"status" binding:"omitempty,min=0,max=1" doc:"账号状态 0: 禁用 1:	启用"`
	DeptID   *ID    `form:"dept_id" doc:"关联部门id"`
}

// UserCreateReq POST /users 入参（角色分配 g 行联动）。
type UserCreateReq struct {
	Username string  `json:"username" binding:"required,min=2,max=30" minLength:"2" maxLength:"30" doc:"用户真实姓名"`
	Password string  `json:"password" binding:"required,min=6,max=64" minLength:"6" maxLength:"64" doc:"用户密码"`
	Nickname string  `json:"nickname" binding:"required,min=1,max=30" minLength:"1" maxLength:"30" doc:"用户昵称"`
	DeptID   ID      `json:"deptId" binding:"required" doc:"关联部门id"`
	PostIDs  []ID    `json:"postIds,omitempty" doc:"关联岗位id列表"`
	RoleIDs  []ID    `json:"roleIds,omitempty" doc:"关联角色id列表"`
	Email    string  `json:"email,omitempty" binding:"omitempty,email,max=50" format:"email" maxLength:"50" doc:"邮箱号码"`
	Mobile   *string `json:"mobile,omitempty" binding:"omitempty,max=20" maxLength:"20" doc:"手机号码"`
	Gender   int8    `json:"gender,omitempty" binding:"omitempty,min=0,max=2" minimum:"0" maximum:"2" doc:"性别0未知 1男 2女"`
	Avatar   string  `json:"avatar,omitempty" binding:"omitempty,max=512" maxLength:"512" doc:"头像"`
	Remark   string  `json:"remark,omitempty" binding:"omitempty,max=500" maxLength:"500" doc:"备注"`
	Status   int8    `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1" default:"1" doc:"用户状态 0：禁用 1：启用"`
}

// UserUpdateReq PUT /users/:id 入参（username 可改，密码走重置接口）。
type UserUpdateReq struct {
	Username string  `json:"username" binding:"required,min=2,max=30" minLength:"2" maxLength:"30" doc:"用户真实姓名"`
	Nickname string  `json:"nickname" binding:"required,min=1,max=30" minLength:"1" maxLength:"30" doc:"用户昵称"`
	DeptID   ID      `json:"deptId" binding:"required" doc:"关联部门id"`
	PostIDs  []ID    `json:"postIds,omitempty" doc:"关联岗位id列表"`
	RoleIDs  []ID    `json:"roleIds,omitempty" doc:"关联角色id列表"`
	Email    string  `json:"email,omitempty" binding:"omitempty,email,max=50" format:"email" maxLength:"50" doc:"邮箱号码"`
	Mobile   *string `json:"mobile,omitempty" binding:"omitempty,max=20" maxLength:"20" doc:"手机号码"`
	Gender   int8    `json:"gender,omitempty" binding:"omitempty,min=0,max=2" minimum:"0" maximum:"2" doc:"性别 0未知 1男 2女"`
	Avatar   string  `json:"avatar,omitempty" binding:"omitempty,max=512" maxLength:"512" doc:"头像"`
	Remark   string  `json:"remark,omitempty" binding:"omitempty,max=500" maxLength:"500" doc:"备注"`
	Status   int8    `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1" default:"1" doc:"用户状态 0：禁用 1：启用"`
}

// ResetPasswordReq PUT /users/:id/password 入参（重置后踢下线）。
type ResetPasswordReq struct {
	Password string `json:"password" binding:"required,min=6,max=64" minLength:"6" maxLength:"64" doc:"用户密码"`
}

// UserDetailResp GET /users/:id 响应：基本信息 + 角色/岗位 ID。
type UserDetailResp struct {
	UserDTO
	RoleIDs []ID `json:"roleIds" doc:"关联角色id列表"`
	PostIDs []ID `json:"postIds" doc:"关联岗位id列表"`
}
