// 角色/菜单/部门/岗位管理模块 DTO（P6）。
package types

import "time"

// ---- 角色 ----

// RolePageQuery GET /roles 查询参数。
type RolePageQuery struct {
	PageNum  int    `form:"page_num" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Name     string `form:"name" binding:"omitempty,max=30"`
	Code     string `form:"code" binding:"omitempty,max=100"`
	Status   *int8  `form:"status" binding:"omitempty,min=0,max=1"`
}

// RoleSaveReq POST/PUT /roles 入参（code 创建后不可改，更新时忽略）。
type RoleSaveReq struct {
	Name             string `json:"name" binding:"required,min=1,max=30"`
	Code             string `json:"code" binding:"omitempty,min=2,max=100"`
	Sort             int    `json:"sort"`
	DataScope        int8   `json:"data_scope" binding:"omitempty,min=1,max=5"`
	DataScopeDeptIDs string `json:"data_scope_dept_ids" binding:"omitempty,max=500"`
	Status           int8   `json:"status" binding:"omitempty,min=0,max=1"`
	Remark           string `json:"remark" binding:"omitempty,max=500"`
}

// RoleAssignMenusReq PUT /roles/:id/menus 入参（重建 role_menu + p 行）。
type RoleAssignMenusReq struct {
	MenuIDs []ID `json:"menuIds"`
}

// RoleDTO 角色信息。
type RoleDTO struct {
	ID               ID        `json:"id"`
	Name             string    `json:"name"`
	Code             string    `json:"code"`
	Sort             int       `json:"sort"`
	DataScope        int8      `json:"dataScope"`
	DataScopeDeptIDs string    `json:"dataScopeDeptIds"`
	Status           int8      `json:"status"`
	Type             int8      `json:"type"` // 1内置 2自定义
	Remark           string    `json:"remark"`
	CreateTime       time.Time `json:"createTime"`
}

// RoleDetailResp GET /roles/:id 响应：角色 + 已分配菜单 ID。
type RoleDetailResp struct {
	RoleDTO
	MenuIDs []ID `json:"menuIds"`
}

// ---- 菜单 ----

// MenuSaveReq POST/PUT /menus 入参。
type MenuSaveReq struct {
	Name          string `json:"name" binding:"required,min=1,max=50"`
	Permission    string `json:"permission" binding:"omitempty,max=100"`
	Type          int8   `json:"type" binding:"required,min=1,max=3"` // 1目录 2菜单 3按钮
	Sort          int    `json:"sort"`
	ParentID      ID     `json:"parentId"`
	Path          string `json:"path" binding:"omitempty,max=200"`
	Icon          string `json:"icon" binding:"omitempty,max=100"`
	Component     string `json:"component" binding:"omitempty,max=255"`
	ComponentName string `json:"component_name" binding:"omitempty,max=255"`
	Status        int8   `json:"status" binding:"omitempty,min=0,max=1"`
	Visible       *bool  `json:"visible"`
	KeepAlive     *bool  `json:"keep_alive"`
	AlwaysShow    *bool  `json:"always_show"`
}

// MenuDTO 菜单信息。
type MenuDTO struct {
	ID            ID        `json:"id"`
	Name          string    `json:"name"`
	Permission    string    `json:"permission"`
	Type          int8      `json:"type"`
	Sort          int       `json:"sort"`
	ParentID      ID        `json:"parentId"`
	Path          string    `json:"path"`
	Icon          string    `json:"icon"`
	Component     string    `json:"component"`
	ComponentName string    `json:"componentName"`
	Status        int8      `json:"status"`
	Visible       bool      `json:"visible"`
	KeepAlive     bool      `json:"keepAlive"`
	AlwaysShow    bool      `json:"alwaysShow"`
	CreateTime    time.Time `json:"createTime"`
}

// MenuTreeResp 菜单树节点。
type MenuTreeResp struct {
	MenuDTO
	Children []MenuTreeResp `json:"children"`
}

// ---- 部门 ----

// DeptSaveReq POST/PUT /depts 入参。
type DeptSaveReq struct {
	Name         string `json:"name" binding:"required,min=1,max=30"`
	ParentID     ID     `json:"parentId"`
	Sort         int    `json:"sort"`
	LeaderUserID ID     `json:"leader_user_id"`
	Phone        string `json:"phone" binding:"omitempty,max=20"`
	Email        string `json:"email" binding:"omitempty,email,max=50"`
	Status       int8   `json:"status" binding:"omitempty,min=0,max=1"`
}

// DeptDTO 部门信息。
type DeptDTO struct {
	ID           ID        `json:"id"`
	Name         string    `json:"name"`
	ParentID     ID        `json:"parentId"`
	Sort         int       `json:"sort"`
	LeaderUserID ID        `json:"leaderUserId"`
	Phone        string    `json:"phone"`
	Email        string    `json:"email"`
	Status       int8      `json:"status"`
	CreateTime   time.Time `json:"createTime"`
}

// DeptTreeResp 部门树节点（内存组装）。
type DeptTreeResp struct {
	DeptDTO
	Children []DeptTreeResp `json:"children"`
}

// ---- 岗位 ----

// PostPageQuery GET /posts 查询参数。
type PostPageQuery struct {
	PageNum  int    `form:"page_num" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Code     string `form:"code" binding:"omitempty,max=64"`
	Name     string `form:"name" binding:"omitempty,max=50"`
	Status   *int8  `form:"status" binding:"omitempty,min=0,max=1"`
}

// PostSaveReq POST/PUT /posts 入参（code 创建后不可改）。
type PostSaveReq struct {
	Code   string `json:"code" binding:"required,min=1,max=64"`
	Name   string `json:"name" binding:"required,min=1,max=50"`
	Sort   int    `json:"sort"`
	Status int8   `json:"status" binding:"omitempty,min=0,max=1"`
	Remark string `json:"remark" binding:"omitempty,max=500"`
}

// PostDTO 岗位信息。
type PostDTO struct {
	ID         ID        `json:"id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	Sort       int       `json:"sort"`
	Status     int8      `json:"status"`
	Remark     string    `json:"remark"`
	CreateTime time.Time `json:"createTime"`
}
