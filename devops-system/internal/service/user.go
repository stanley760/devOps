// 用户管理服务：CRUD + casbin g 行联动。
//
// 一致性策略：业务行（user + user_role）走 DB 事务；casbin g 行在事务提交后重建。
// g 行写失败只造成「权限缺失」而非越权（安全方向正确），返回错误提示重试。
package service

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"devops-infra/errs"

	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/dao"
	"devops-infra/datascope"
	"devops-system/internal/model"
	"devops-system/internal/types"
)

// ensureUserVisible 单点数据权限收口（Get/Update/ResetPassword/Delete 按 ID 直查后判定）：
// 越权一律按 404 呈现（与「用户不存在」同一响应，防存在性探测）。
// ctx 未注入数据权限时 fail-closed 报错（受控路由已挂 HumaDataScope，漏配即暴露）。
func ensureUserVisible(ctx context.Context, u *model.User) error {
	scope, ok := datascope.From(ctx)
	if !ok {
		return errs.New(errs.CodeInternal, "缺少数据权限上下文（受控接口未挂解析中间件）")
	}
	if !scope.Visible(u.DeptID, u.ID) {
		return errs.New(errs.CodeNotFound, "用户不存在")
	}
	return nil
}

// User 用户管理服务。
type User struct {
	authM           *auth.Manager
	invalidateScope func() // 数据权限缓存失效（角色分配变更时调用）
}

// NewUser 创建服务（authM 用于踢下线；invalidateScope 清数据权限缓存）。
func NewUser(authM *auth.Manager, invalidateScope func()) *User {
	return &User{authM: authM, invalidateScope: invalidateScope}
}

// List 分页查询（tenant_id 由插件注入，天然限本租户）。
func (s *User) List(ctx context.Context, q types.UserPageQuery) ([]types.UserDTO, int64, error) {
	users, total, err := dao.ListUsers(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]types.UserDTO, 0, len(users))
	for i := range users {
		out = append(out, toUserDTO(&users[i]))
	}
	return out, total, nil
}

// Get 用户详情（基本信息 + 角色/岗位 ID；数据权限单点收口）。
func (s *User) Get(ctx context.Context, id types.ID) (*types.UserDetailResp, error) {
	u, err := dao.GetUserByID(ctx, id.Int64())
	if err != nil {
		return nil, err
	}
	if err := ensureUserVisible(ctx, u); err != nil {
		return nil, err
	}
	roleIDs, err := dao.GetUserRoleIDs(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	rids := make([]types.ID, 0, len(roleIDs))
	for _, rid := range roleIDs {
		rids = append(rids, types.ID(rid))
	}
	return &types.UserDetailResp{
		UserDTO: toUserDTO(u),
		RoleIDs: rids,
		PostIDs: dao.PostIDsOf(u.PostIDs),
	}, nil
}

// Create 创建用户：租户内唯一校验 → 发号落库 → 角色 g 行联动。
func (s *User) Create(ctx context.Context, claims *auth.AccessClaims, req types.UserCreateReq) error {
	exists, err := dao.UsernameExists(ctx, req.Username, 0)
	if err != nil {
		return err
	}
	if exists {
		return errs.New(errs.CodeParam, "用户名已存在")
	}
	if req.Mobile != nil {
		exists, err = dao.MobileExists(ctx, *req.Mobile, 0)
		if err != nil {
			return err
		}
		if exists {
			return errs.New(errs.CodeParam, "手机号已存在")
		}
	}
	roleCodes, err := dao.RoleCodesByIDs(ctx, idsToInt64(req.RoleIDs))
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "密码加密失败")
	}

	u := &model.User{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		},
		Username: req.Username,
		Password: string(hash),
		Nickname: req.Nickname,
		DeptID:   req.DeptID.Int64(),
		PostIDs:  dao.PostIDsJSON(req.PostIDs),
		Email:    req.Email,
		Mobile:   req.Mobile,
		Sex:      req.Sex,
		Avatar:   req.Avatar,
		Remark:   req.Remark,
		Status:   req.Status,
	}
	if err := dao.CreateUserWithRoles(ctx, u, idsToInt64(req.RoleIDs)); err != nil {
		return err
	}

	if len(roleCodes) > 0 {
		if err := authz.RebuildUserGrouping(u.ID, claims.TenantID, roleCodes); err != nil {
			return errs.Wrap(err, errs.CodeInternal, "用户已创建，但角色授权写入失败，请重试更新")
		}
	}
	return nil
}

// Update 更新用户（可改 username/角色/状态等；停用即踢下线；数据权限单点收口）。
func (s *User) Update(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.UserUpdateReq) error {
	u, err := dao.GetUserByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	if err := ensureUserVisible(ctx, u); err != nil {
		return err
	}
	if u.Username != req.Username {
		exists, err := dao.UsernameExists(ctx, req.Username, u.ID)
		if err != nil {
			return err
		}
		if exists {
			return errs.New(errs.CodeParam, "用户名已存在")
		}
	}
	if req.Mobile != nil && *req.Mobile != "" && (u.Mobile == nil || *u.Mobile != *req.Mobile) {
		exists, err := dao.MobileExists(ctx, *req.Mobile, u.ID)
		if err != nil {
			return err
		}
		if exists {
			return errs.New(errs.CodeParam, "手机号已存在")
		}
	}
	roleCodes, err := dao.RoleCodesByIDs(ctx, idsToInt64(req.RoleIDs))
	if err != nil {
		return err
	}

	updates := map[string]any{
		"username": req.Username,
		"nickname": req.Nickname,
		"dept_id":  req.DeptID.Int64(),
		"email":    req.Email,
		"sex":      req.Sex,
		"avatar":   req.Avatar,
		"remark":   req.Remark,
		"status":   req.Status,
		"updater":  claims.Username,
	}
	if pj := dao.PostIDsJSON(req.PostIDs); pj != "" {
		updates["post_ids"] = pj
	} else {
		updates["post_ids"] = nil // 空岗位存 NULL（json 列拒绝 ''）
	}
	if req.Mobile != nil && *req.Mobile != "" {
		updates["mobile"] = *req.Mobile
	} else {
		updates["mobile"] = nil // 清空走 NULL，保 uk_tenant_mobile 多空值不冲突
	}

	if err := dao.UpdateUserWithRoles(ctx, u.ID, updates, idsToInt64(req.RoleIDs)); err != nil {
		return err
	}

	if err := authz.RebuildUserGrouping(u.ID, claims.TenantID, roleCodes); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "用户已更新，但角色授权刷新失败，请重试更新")
	}
	// 角色分配已变：清数据权限缓存（含部门调动改变可见范围）
	s.invalidateScope()
	// 状态 1→0：停用即踢下线（存量会话立即失效）
	if u.Status == 1 && req.Status == 0 {
		s.authM.RevokeAllSessions(ctx, claims.TenantID, u.ID)
	}
	return nil
}

// ResetPassword 重置密码：更新 + 撤销全部会话（强制重新登录；数据权限单点收口）。
func (s *User) ResetPassword(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.ResetPasswordReq) error {
	u, err := dao.GetUserByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	if err := ensureUserVisible(ctx, u); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "密码加密失败")
	}
	if err := dao.UpdateUser(ctx, u.ID, map[string]any{
		"password": string(hash),
		"updater":  claims.Username,
	}); err != nil {
		return err
	}
	s.authM.RevokeAllSessions(ctx, claims.TenantID, u.ID)
	return nil
}

// Delete 删除用户：防护（自己/内置超管）→ 软删（改写唯一键）→ 清关联与 g 行 → 踢下线。
func (s *User) Delete(ctx context.Context, claims *auth.AccessClaims, id types.ID) error {
	if id.Int64() == claims.UserID {
		return errs.New(errs.CodeParam, "不能删除当前登录用户")
	}
	if id.Int64() == 1 {
		return errs.New(errs.CodeParam, "内置管理员不可删除")
	}
	u, err := dao.GetUserByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	if err := ensureUserVisible(ctx, u); err != nil {
		return err
	}

	if err := dao.DeleteUser(ctx, u, claims.Username); err != nil {
		return err
	}

	if err := authz.RemoveUserGrouping(u.ID, claims.TenantID); err != nil {
		return errs.Wrap(err, errs.CodeInternal, "用户已删除，但角色授权清理失败")
	}
	s.authM.RevokeAllSessions(ctx, claims.TenantID, u.ID)
	return nil
}

// toUserDTO model.User → DTO（隐藏密码）。
func toUserDTO(u *model.User) types.UserDTO {
	dto := types.UserDTO{
		ID:       types.ID(u.ID),
		Username: u.Username,
		Nickname: u.Nickname,
		Email:    u.Email,
		Sex:      u.Sex,
		Avatar:   u.Avatar,
		DeptID:   types.ID(u.DeptID),
		PostIDs:  dao.PostIDsOf(u.PostIDs),
		Remark:   u.Remark,
		LoginIP:  u.LoginIP,
	}
	if u.Mobile != nil {
		dto.Mobile = *u.Mobile
	}
	if u.LoginDate != nil {
		dto.LoginDate = *u.LoginDate
	}
	return dto
}

// idsToInt64 []ID → []int64。
func idsToInt64(ids []types.ID) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.Int64())
	}
	return out
}
