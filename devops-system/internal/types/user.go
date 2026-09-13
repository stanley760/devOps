// 用户管理模块 DTO。ID 一律 types.ID 字符串形态。
package types

// UserPageQuery GET /users 查询参数。
type UserPageQuery struct {
	PageNum  int    `form:"page_num" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Username string `form:"username" binding:"omitempty,max=30"`
	Mobile   string `form:"mobile" binding:"omitempty,max=20"`
	Status   *int8  `form:"status" binding:"omitempty,min=0,max=1"`
	DeptID   *ID    `form:"dept_id"`
}

// UserCreateReq POST /users 入参（角色分配 g 行联动）。
type UserCreateReq struct {
	Username string  `json:"username" binding:"required,min=2,max=30"`
	Password string  `json:"password" binding:"required,min=6,max=64"`
	Nickname string  `json:"nickname" binding:"required,min=1,max=30"`
	DeptID   ID      `json:"deptId" binding:"required"`
	PostIDs  []ID    `json:"postIds"`
	RoleIDs  []ID    `json:"roleIds"`
	Email    string  `json:"email" binding:"omitempty,email,max=50"`
	Mobile   *string `json:"mobile" binding:"omitempty,max=20"`
	Sex      int8    `json:"sex" binding:"omitempty,min=0,max=2"`
	Avatar   string  `json:"avatar" binding:"omitempty,max=512"`
	Remark   string  `json:"remark" binding:"omitempty,max=500"`
	Status   int8    `json:"status" binding:"omitempty,min=0,max=1"`
}

// UserUpdateReq PUT /users/:id 入参（username 可改，密码走重置接口）。
type UserUpdateReq struct {
	Username string  `json:"username" binding:"required,min=2,max=30"`
	Nickname string  `json:"nickname" binding:"required,min=1,max=30"`
	DeptID   ID      `json:"deptId" binding:"required"`
	PostIDs  []ID    `json:"postIds"`
	RoleIDs  []ID    `json:"roleIds"`
	Email    string  `json:"email" binding:"omitempty,email,max=50"`
	Mobile   *string `json:"mobile" binding:"omitempty,max=20"`
	Sex      int8    `json:"sex" binding:"omitempty,min=0,max=2"`
	Avatar   string  `json:"avatar" binding:"omitempty,max=512"`
	Remark   string  `json:"remark" binding:"omitempty,max=500"`
	Status   int8    `json:"status" binding:"omitempty,min=0,max=1"`
}

// ResetPasswordReq PUT /users/:id/password 入参（重置后踢下线）。
type ResetPasswordReq struct {
	Password string `json:"password" binding:"required,min=6,max=64"`
}

// UserDetailResp GET /users/:id 响应：基本信息 + 角色/岗位 ID。
type UserDetailResp struct {
	UserDTO
	RoleIDs []ID `json:"roleIds"`
	PostIDs []ID `json:"postIds"`
}
