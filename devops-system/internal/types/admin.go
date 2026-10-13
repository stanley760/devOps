// 角色/菜单/部门/岗位管理模块 DTO。
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
	Name             string `json:"name" binding:"required,min=1,max=30" minLength:"1" maxLength:"30"`
	Code             string `json:"code,omitempty" binding:"omitempty,min=2,max=100" minLength:"2" maxLength:"100"`
	Sort             int    `json:"sort,omitempty"`
	DataScope        int8   `json:"data_scope,omitempty" binding:"omitempty,min=1,max=5" minimum:"1" maximum:"5"`
	DataScopeDeptIDs string `json:"data_scope_dept_ids,omitempty" binding:"omitempty,max=500" maxLength:"500"`
	Status           int8   `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1"`
	Remark           string `json:"remark,omitempty" binding:"omitempty,max=500" maxLength:"500"`
}

// RoleAssignMenusReq PUT /roles/:id/menus 入参（重建 role_menu + p 行）。
type RoleAssignMenusReq struct {
	MenuIDs []ID `json:"menuIds,omitempty"`
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
	Name          string `json:"name" binding:"required,min=1,max=50" minLength:"1" maxLength:"50"`
	Permission    string `json:"permission,omitempty" binding:"omitempty,max=100" maxLength:"100"`
	Type          int8   `json:"type" binding:"required,min=1,max=3" minimum:"1" maximum:"3"` // 1目录 2菜单 3按钮
	Sort          int    `json:"sort,omitempty"`
	ParentID      ID     `json:"parentId,omitempty"`
	Path          string `json:"path,omitempty" binding:"omitempty,max=200" maxLength:"200"`
	Icon          string `json:"icon,omitempty" binding:"omitempty,max=100" maxLength:"100"`
	Component     string `json:"component,omitempty" binding:"omitempty,max=255" maxLength:"255"`
	ComponentName string `json:"component_name,omitempty" binding:"omitempty,max=255" maxLength:"255"`
	Status        int8   `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1"`
	Visible       *bool  `json:"visible,omitempty"`
	KeepAlive     *bool  `json:"keep_alive,omitempty"`
	AlwaysShow    *bool  `json:"always_show,omitempty"`
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
	Name         string `json:"name" binding:"required,min=1,max=30" minLength:"1" maxLength:"30"`
	ParentID     ID     `json:"parentId,omitempty"`
	Sort         int    `json:"sort,omitempty"`
	LeaderUserID ID     `json:"leader_user_id,omitempty"`
	Phone        string `json:"phone,omitempty" binding:"omitempty,max=20" maxLength:"20"`
	Email        string `json:"email,omitempty" binding:"omitempty,email,max=50" format:"email" maxLength:"50"`
	Status       int8   `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1"`
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
	PageNum  int    `form:"page_num" binding:"omitempty,min=1" default:"1" doc:"页码"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=999" default:"10" doc:"页数"`
	Code     string `form:"code" binding:"omitempty,max=64" doc:"岗位编码"`
	Name     string `form:"name" binding:"omitempty,max=50" doc:"岗位名称"`
	Status   *int8  `form:"status" binding:"omitempty,min=0,max=1" doc:"岗位状态" default:"1"`
}

// PostSaveReq POST/PUT /posts 入参（code 创建后不可改）。
type PostSaveReq struct {
	Code   string `json:"code" binding:"required,min=1,max=64" minLength:"1" maxLength:"64" doc:"岗位编号"`
	Name   string `json:"name" binding:"required,min=1,max=50" minLength:"1" maxLength:"50" doc:"岗位名称"`
	Sort   int    `json:"sort,omitempty" doc:"排序"`
	Status int8   `json:"status,omitempty" binding:"omitempty,min=0,max=1" doc:"岗位状态 0:禁用；1:启用"`
	Remark string `json:"remark,omitempty" binding:"omitempty,max=500" maxLength:"500" doc:"备注"`
}

// PostDTO 岗位信息。
type PostDTO struct {
	ID         ID        `json:"id" doc:"岗位id"`
	Code       string    `json:"code" doc:"岗位编号"`
	Name       string    `json:"name" doc:"岗位名称"`
	Sort       int       `json:"sort" doc:"排序"`
	Status     int8      `json:"status" doc:"岗位状态 0:禁用；1:启用"`
	Remark     string    `json:"remark" doc:"备注"`
	CreateTime time.Time `json:"createTime" doc:"创建日期"`
}
