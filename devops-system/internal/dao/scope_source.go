package dao

import (
	"context"

	"github.com/stanley760/devOps/devops-infra/datascope"
)

// 数据权限解析的数据源适配：把 model.X 转成 infra 值对象（datascope.RoleScope /
// UserRef / DeptNode），供中间件 Kit 的 Deps 绑定。
//
// model 只在 dao 层出现，middleware/humakit 只见值对象——这是 Kit 能下沉到
// devops-infra 的前提（见 devops-infra/security/humakit）。

// UserScopes 取用户在租户内的角色数据权限声明（datascope.RoleScope 视图）。
func UserScopes(ctx context.Context, userID int64) ([]datascope.RoleScope, error) {
	roles, err := GetUserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]datascope.RoleScope, 0, len(roles))
	for _, r := range roles {
		out = append(out, datascope.RoleScope{
			Code:             r.Code,
			DataScope:        r.DataScope,
			DataScopeDeptIDs: r.DataScopeDeptIDs,
		})
	}
	return out, nil
}

// UserRef 取数据权限解析所需的用户最小视图（ID + 部门）。
func UserRef(ctx context.Context, userID int64) (datascope.UserRef, error) {
	u, err := GetUserByID(ctx, userID)
	if err != nil {
		return datascope.UserRef{}, err
	}
	return datascope.UserRef{ID: u.ID, DeptID: u.DeptID}, nil
}

// DeptNodes 取部门树最小视图（子树展开用）。
func DeptNodes(ctx context.Context) ([]datascope.DeptNode, error) {
	depts, err := ListDepts(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]datascope.DeptNode, 0, len(depts))
	for _, d := range depts {
		out = append(out, datascope.DeptNode{ID: d.ID, ParentID: d.ParentID})
	}
	return out, nil
}
