// 角色/菜单/部门/岗位管理模块 DTO。
package types

import (
	"time"

	"github.com/stanley760/devOps/devops-common/domain"
)

// ---- 角色 ----

// RolePageQuery GET /roles 查询参数。
type RolePageQuery struct {
	domain.PageParam
	Name     string `form:"name" binding:"omitempty,max=30" doc:"角色名称"`
	Code     string `form:"code" binding:"omitempty,max=100" doc:"角色编码"`
	Status   *int8  `form:"status" binding:"omitempty,min=0,max=1" doc:"角色状态 0:禁用；1:启用"`
}

// RoleSaveReq POST/PUT /roles 入参（code 创建后不可改，更新时忽略）。
type RoleSaveReq struct {
	Name             string `json:"name" binding:"required,min=1,max=30" minLength:"1" maxLength:"30" doc:"角色名称"`
	Code             string `json:"code,omitempty" binding:"omitempty,min=2,max=100" minLength:"2" maxLength:"100" doc:"角色编码，创建后不可修改"`
	Sort             int    `json:"sort,omitempty" doc:"排序"`
	DataScope        int8   `json:"data_scope,omitempty" binding:"omitempty,min=1,max=5" minimum:"1" maximum:"5" doc:"数据范围 1:全部 2:自定义 3:仅本部门 4:本部门及以下 5:仅本人"`
	DataScopeDeptIDs string `json:"data_scope_dept_ids,omitempty" binding:"omitempty,max=500" maxLength:"500" doc:"数据范围为自定义时的部门 ID 列表，逗号分隔"`
	Status           int8   `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1" doc:"角色状态 0:禁用；1:启用"`
	Remark           string `json:"remark,omitempty" binding:"omitempty,max=500" maxLength:"500" doc:"备注"`
}

// RoleAssignMenusReq PUT /roles/:id/menus 入参（重建 role_menu + p 行）。
type RoleAssignMenusReq struct {
	MenuIDs []ID `json:"menuIds,omitempty" doc:"待分配的菜单 ID 列表"`
}

// RoleDTO 角色信息。
type RoleDTO struct {
	ID               ID        `json:"id" doc:"角色id"`
	Name             string    `json:"name" doc:"角色名称"`
	Code             string    `json:"code" doc:"角色编码"`
	Sort             int       `json:"sort" doc:"排序"`
	DataScope        int8      `json:"dataScope" doc:"数据范围 1:全部 2:自定义 3:仅本部门 4:本部门及以下 5:仅本人"`
	DataScopeDeptIDs string    `json:"dataScopeDeptIds" doc:"数据范围为自定义时的部门 ID 列表，逗号分隔"`
	Status           int8      `json:"status" doc:"角色状态 0:禁用；1:启用"`
	Type             int8      `json:"type" doc:"角色类型 1:内置 2:自定义"`
	Remark           string    `json:"remark" doc:"备注"`
	CreateTime       time.Time `json:"createTime" doc:"创建日期"`
}

// RoleDetailResp GET /roles/:id 响应：角色 + 已分配菜单 ID。
type RoleDetailResp struct {
	RoleDTO
	MenuIDs []ID `json:"menuIds" doc:"已分配的菜单 ID 列表"`
}

// ---- 菜单 ----

// MenuSaveReq POST/PUT /menus 入参。
type MenuSaveReq struct {
	Name          string `json:"name" binding:"required,min=1,max=50" minLength:"1" maxLength:"50" doc:"菜单名称"`
	Permission    string `json:"permission,omitempty" binding:"omitempty,max=100" maxLength:"100" doc:"权限标识"`
	Type          int8   `json:"type" binding:"required,min=1,max=3" minimum:"1" maximum:"3" doc:"菜单类型 1:目录 2:菜单 3:按钮"`
	Sort          int    `json:"sort,omitempty" doc:"排序"`
	ParentID      ID     `json:"parentId,omitempty" doc:"父级菜单id，顶级为 0"`
	Path          string `json:"path,omitempty" binding:"omitempty,max=200" maxLength:"200" doc:"路由路径"`
	Icon          string `json:"icon,omitempty" binding:"omitempty,max=100" maxLength:"100" doc:"图标"`
	Component     string `json:"component,omitempty" binding:"omitempty,max=255" maxLength:"255" doc:"组件路径"`
	ComponentName string `json:"component_name,omitempty" binding:"omitempty,max=255" maxLength:"255" doc:"组件名称"`
	Status        int8   `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1" doc:"菜单状态 0:禁用；1:启用"`
	Visible       *bool  `json:"visible,omitempty" doc:"是否可见"`
	KeepAlive     *bool  `json:"keep_alive,omitempty" doc:"是否缓存"`
	AlwaysShow    *bool  `json:"always_show,omitempty" doc:"是否总是显示"`
}

// MenuDTO 菜单信息。
type MenuDTO struct {
	ID            ID        `json:"id" doc:"菜单id"`
	Name          string    `json:"name" doc:"菜单名称"`
	Permission    string    `json:"permission" doc:"权限标识"`
	Type          int8      `json:"type" doc:"菜单类型 1:目录 2:菜单 3:按钮"`
	Sort          int       `json:"sort" doc:"排序"`
	ParentID      ID        `json:"parentId" doc:"父级菜单id，顶级为 0"`
	Path          string    `json:"path" doc:"路由路径"`
	Icon          string    `json:"icon" doc:"图标"`
	Component     string    `json:"component" doc:"组件路径"`
	ComponentName string    `json:"componentName" doc:"组件名称"`
	Status        int8      `json:"status" doc:"菜单状态 0:禁用；1:启用"`
	Visible       bool      `json:"visible" doc:"是否可见"`
	KeepAlive     bool      `json:"keepAlive" doc:"是否缓存"`
	AlwaysShow    bool      `json:"alwaysShow" doc:"是否总是显示"`
	CreateTime    time.Time `json:"createTime" doc:"创建日期"`
}

// MenuTreeResp 菜单树节点。
type MenuTreeResp struct {
	MenuDTO
	Children []MenuTreeResp `json:"children" doc:"子菜单"`
}

// ---- 部门 ----

// DeptSaveReq POST/PUT /depts 入参。
type DeptSaveReq struct {
	Name         string `json:"name" binding:"required,min=1,max=30" minLength:"1" maxLength:"30" doc:"部门名称"`
	ParentID     ID     `json:"parentId,omitempty" doc:"父级部门id，顶级为 0"`
	Sort         int    `json:"sort,omitempty" doc:"排序"`
	LeaderUserID ID     `json:"leader_user_id,omitempty" doc:"负责人用户id"`
	Phone        string `json:"phone,omitempty" binding:"omitempty,max=20" maxLength:"20" doc:"联系电话"`
	Email        string `json:"email,omitempty" binding:"omitempty,email,max=50" format:"email" maxLength:"50" doc:"邮箱"`
	Status       int8   `json:"status,omitempty" binding:"omitempty,min=0,max=1" minimum:"0" maximum:"1" doc:"部门状态 0:禁用；1:启用"`
}

// DeptDTO 部门信息。
type DeptDTO struct {
	ID           ID        `json:"id" doc:"部门id"`
	Name         string    `json:"name" doc:"部门名称"`
	ParentID     ID        `json:"parentId" doc:"父级部门id，顶级为 0"`
	Sort         int       `json:"sort" doc:"排序"`
	LeaderUserID ID        `json:"leaderUserId" doc:"负责人用户id"`
	Phone        string    `json:"phone" doc:"联系电话"`
	Email        string    `json:"email" doc:"邮箱"`
	Status       int8      `json:"status" doc:"部门状态 0:禁用；1:启用"`
	CreateTime   time.Time `json:"createTime" doc:"创建日期"`
}

// DeptTreeResp 部门树节点（内存组装）。
type DeptTreeResp struct {
	DeptDTO
	Children []DeptTreeResp `json:"children" doc:"子部门"`
}

// ---- 岗位 ----

// PostPageQuery GET /posts 查询参数。
type PostPageQuery struct {
	domain.PageParam
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
