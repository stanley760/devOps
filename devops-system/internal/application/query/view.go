package query

import (
	commonaudit "github.com/stanley760/devOps/devops-common/audit"
	"github.com/stanley760/devOps/devops-common/domain"

	"github.com/stanley760/devOps/devops-system/internal/domain/auditlog"
	"github.com/stanley760/devOps/devops-system/internal/domain/iam"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

// 领域对象 → API DTO 的视图映射。
//
// 改造前这些映射散落在 service 层，共 8 处手写 toXxxDTO（另有两处内联
// 重复实现，其中 service/auth.go:235-250 的 toUserDTO 副本与
// service/user.go:258 的版本字段几乎一一对应，只是一个用 derefString、
// 一个用 if 判空——典型的映射漂移隐患）。
//
// 现在全部收敛到本文件，且用户映射只有一份。

// idsToIDs int64 → []types.ID。
func idsToIDs(ids []int64) []types.ID {
	out := make([]types.ID, 0, len(ids))
	for _, id := range ids {
		out = append(out, types.ID(id))
	}
	return out
}

// derefString 解引用可空字符串（空指针 → 空串）。
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---- 用户 ----

// UserDTO 领域用户 → API DTO（不含口令）。
func UserDTO(u *iam.User) types.UserDTO {
	dto := types.UserDTO{
		ID:       types.ID(u.ID),
		Username: u.Username,
		Nickname: u.Nickname,
		Email:    u.Email,
		Mobile:   derefString(u.Mobile),
		Gender:   u.Gender,
		Avatar:   u.Avatar,
		DeptID:   types.ID(u.DeptID),
		PostIDs:  idsToIDs(u.PostIDs),
		Remark:   u.Remark,
		LoginIP:  u.LoginIP,
	}
	if u.LoginDate != nil {
		dto.LoginDate = *u.LoginDate
	}
	return dto
}

// UserDetail 用户详情（基本信息 + 角色/岗位 ID）。
func UserDetail(u *iam.User) *types.UserDetailResp {
	dto := UserDTO(u)
	return &types.UserDetailResp{
		UserDTO: dto,
		RoleIDs: idsToIDs(u.RoleIDs),
		PostIDs: dto.PostIDs,
	}
}

// ---- 角色 ----

// RoleDTO 领域角色 → API DTO。
func RoleDTO(r *iam.Role) types.RoleDTO {
	return types.RoleDTO{
		ID:               types.ID(r.ID),
		Name:             r.Name,
		Code:             r.Code,
		Sort:             r.Sort,
		DataScope:        r.DataScope,
		DataScopeDeptIDs: r.DataScopeDeptIDs,
		Status:           r.Status,
		Type:             r.Type,
		Remark:           r.Remark,
		CreateTime:       r.CreateTime,
	}
}

// RoleDetail 角色详情（含已分配菜单 ID）。
func RoleDetail(r *iam.Role) *types.RoleDetailResp {
	return &types.RoleDetailResp{
		RoleDTO: RoleDTO(r),
		MenuIDs: idsToIDs(r.MenuIDs),
	}
}

// ---- 菜单 ----

// MenuDTO 领域菜单 → API DTO。
func MenuDTO(m *iam.Menu) types.MenuDTO {
	return types.MenuDTO{
		ID:            types.ID(m.ID),
		Name:          m.Name,
		Permission:    m.Permission,
		Type:          m.Type,
		Sort:          m.Sort,
		ParentID:      types.ID(m.ParentID),
		Path:          m.Path,
		Icon:          m.Icon,
		Component:     m.Component,
		ComponentName: m.ComponentName,
		Status:        m.Status,
		Visible:       m.Visible,
		KeepAlive:     m.KeepAlive,
		AlwaysShow:    m.AlwaysShow,
		CreateTime:    m.CreateTime,
	}
}

// MenuTree 组装菜单树（复用泛型 BuildTree，消除改造前 service/menu.go 的重复实现）。
func MenuTree(menus []*iam.Menu) []types.MenuTreeResp {
	nodes := BuildTree(menus,
		func(m *iam.Menu) int64 { return m.ID },
		func(m *iam.Menu) int64 { return m.ParentID },
		func(m *iam.Menu) int { return m.Sort },
	)
	return mapNodes(nodes, func(m *iam.Menu) types.MenuTreeResp {
		return types.MenuTreeResp{MenuDTO: MenuDTO(m)}
	}, func(n *types.MenuTreeResp, cs []types.MenuTreeResp) { n.Children = cs })
}

// ---- 部门 ----

// DeptDTO 领域部门 → API DTO。
func DeptDTO(d *iam.Dept) types.DeptDTO {
	return types.DeptDTO{
		ID:           types.ID(d.ID),
		Name:         d.Name,
		ParentID:     types.ID(d.ParentID),
		Sort:         d.Sort,
		LeaderUserID: types.ID(d.LeaderUserID),
		Phone:        d.Phone,
		Email:        d.Email,
		Status:       d.Status,
		CreateTime:   d.CreateTime,
	}
}

// DeptTree 组装部门树。
func DeptTree(depts []*iam.Dept) []types.DeptTreeResp {
	nodes := BuildTree(depts,
		func(d *iam.Dept) int64 { return d.ID },
		func(d *iam.Dept) int64 { return d.ParentID },
		func(d *iam.Dept) int { return d.Sort },
	)
	return mapNodes(nodes, func(d *iam.Dept) types.DeptTreeResp {
		return types.DeptTreeResp{DeptDTO: DeptDTO(d)}
	}, func(n *types.DeptTreeResp, cs []types.DeptTreeResp) { n.Children = cs })
}

// ---- 岗位 ----

// PostDTO 领域岗位 → API DTO。
func PostDTO(p *iam.Post) types.PostDTO {
	return types.PostDTO{
		ID:         types.ID(p.ID),
		Code:       p.Code,
		Name:       p.Name,
		Sort:       p.Sort,
		Status:     p.Status,
		Remark:     p.Remark,
		CreateTime: p.CreateTime,
	}
}

// ---- 租户 ----

// TenantDTO 领域租户 → API DTO。
func TenantDTO(t *iam.Tenant) types.TenantDTO {
	return types.TenantDTO{
		ID:            types.ID(t.ID),
		Name:          t.Name,
		Code:          t.Code,
		ContactName:   t.ContactName,
		ContactMobile: t.ContactMobile,
		Status:        t.Status,
		ExpireTime:    t.ExpireTime,
		Remark:        t.Remark,
		CreateTime:    t.CreateTime,
	}
}

// TenantBrief 租户摘要（登录响应内嵌）。
func TenantBrief(t *iam.Tenant) types.TenantBrief {
	return types.TenantBrief{ID: types.ID(t.ID), Name: t.Name, Code: t.Code}
}

// ---- 审计日志 ----

// LoginLogDTO 登录日志 → API DTO。
func LoginLogDTO(l *auditlog.LoginLog) types.LoginLogDTO {
	return types.LoginLogDTO{
		ID:          types.ID(l.ID),
		Username:    l.Username,
		UserID:      types.ID(l.UserID),
		IP:          l.IP,
		UserAgent:   l.UserAgent,
		GeoCountry:  l.GeoCountry,
		GeoProvince: l.GeoProvince,
		GeoCity:     l.GeoCity,
		GeoIsp:      l.GeoISP,
		Result:      l.Result,
		Code:        l.Code,
		CreateTime:  l.CreateTime,
	}
}

// OperationLogDTO 操作日志 → API DTO。
func OperationLogDTO(l *auditlog.OperationLog) types.OperationLogDTO {
	return types.OperationLogDTO{
		ID:            types.ID(l.ID),
		TraceID:       l.TraceID,
		UserID:        types.ID(l.UserID),
		UserName:      l.UserName,
		UserType:      commonaudit.UserType(l.UserType),
		UserTypeName:  commonaudit.UserType(l.UserType).String(),
		Module:        l.Module,
		SubModule:     l.SubModule,
		BizID:         types.ID(l.BizID),
		Contents:      l.Contents,
		Extra:         l.Extra,
		RequestMethod: l.RequestMethod,
		RequestURL:    l.RequestURL,
		UserIP:        l.UserIP,
		UserAgent:     l.UserAgent,
		GeoCountry:    l.GeoCountry,
		GeoProvince:   l.GeoProvince,
		GeoCity:       l.GeoCity,
		GeoIsp:        l.GeoISP,
		Result:        l.Result,
		Code:          l.Code,
		CreateTime:    l.CreateTime,
	}
}

// mapNodes 把泛型节点树映射为 API DTO 树。
func mapNodes[S, D any](nodes []TreeNode[S], wrap func(S) D, attach func(*D, []D)) []D {
	out := make([]D, 0, len(nodes))
	var walk func([]TreeNode[S]) []D
	walk = func(ns []TreeNode[S]) []D {
		if len(ns) == 0 {
			return nil
		}
		res := make([]D, 0, len(ns))
		for _, n := range ns {
			d := wrap(n.Value)
			if len(n.Children) > 0 {
				attach(&d, walk(n.Children))
			}
			res = append(res, d)
		}
		return res
	}
	out = walk(nodes)
	return out
}

// MapPageEntities 领域分页结果 → DTO 分页结果（保留分页元数据）。
func MapPageEntities[S, D any](page domain.PageResult[S], convert func(S) D) domain.PageResult[D] {
	return domain.MapPage(page, convert)
}
