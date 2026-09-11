// Package service 业务逻辑层：认证（登录/刷新/登出/用户信息/个人中心）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"devops-infra/errs"

	"devops-system/internal/auth"
	"devops-system/internal/config"
	"devops-system/internal/dao"
	"devops-system/internal/model"
	"devops-system/internal/tenantdb"
	"devops-system/internal/types"
)

// Auth 认证服务。
type Auth struct {
	cfg   config.Config
	authM *auth.Manager
}

// NewAuth 创建认证服务。
func NewAuth(cfg config.Config, authM *auth.Manager) *Auth {
	return &Auth{cfg: cfg, authM: authM}
}

// Login 登录：租户校验（存在/停用/过期）→ 用户校验（密码/停用）→ 签发双 token。
func (s *Auth) Login(ctx context.Context, req types.LoginReq, clientIP string) (*types.LoginResp, error) {
	code := req.TenantCode
	if code == "" {
		code = s.cfg.Tenant.DefaultCode
	}

	// 公开路由：无租户 ctx，直接查全局表
	var t model.Tenant
	if err := tenantdb.DB(ctx).Where("code = ?", code).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeUnauthorized, "租户不存在")
		}
		return nil, errs.Wrap(err, errs.CodeInternal, "查询租户失败")
	}
	if t.Status != 0 {
		return nil, errs.New(errs.CodeForbidden, "租户已停用")
	}
	if t.ExpireTime != nil && t.ExpireTime.Before(time.Now()) {
		return nil, errs.New(errs.CodeForbidden, "租户已过期")
	}

	u, err := dao.GetUserByUsername(ctx, t.ID, req.Username)
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Password)) != nil {
		return nil, errs.New(errs.CodeUnauthorized, "用户名或密码错误")
	}
	if u.Status != 0 {
		return nil, errs.New(errs.CodeForbidden, "用户已停用")
	}

	access, refresh, expiresIn, err := s.authM.Issue(ctx, u.ID, u.Username, t.ID, t.Code)
	if err != nil {
		return nil, err
	}
	dao.TouchLogin(ctx, t.ID, u.ID, clientIP)

	return &types.LoginResp{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    expiresIn,
		Tenant:       types.TenantBrief{ID: types.ID(t.ID), Name: t.Name, Code: t.Code},
	}, nil
}

// Refresh 轮换会话：sid 定位 → 会话校验 → 用户/租户复检 → 新双 token。
func (s *Auth) Refresh(ctx context.Context, req types.RefreshReq) (*types.TokenResp, error) {
	tid, uid, ok := s.authM.LookupSID(ctx, req.RefreshToken)
	if !ok || !s.authM.SIDMatch(ctx, tid, req.RefreshToken, uid) {
		return nil, errs.New(errs.CodeUnauthorized, "刷新令牌无效或已过期")
	}

	u, err := dao.GetUserByID(ctx, tid, uid)
	if err != nil {
		return nil, errs.New(errs.CodeUnauthorized, "用户不存在或已删除")
	}
	if u.Status != 0 {
		return nil, errs.New(errs.CodeForbidden, "用户已停用")
	}

	var t model.Tenant
	if err := tenantdb.DB(ctx).Where("id = ?", tid).First(&t).Error; err != nil || t.Status != 0 {
		return nil, errs.New(errs.CodeForbidden, "租户不可用")
	}
	if t.ExpireTime != nil && t.ExpireTime.Before(time.Now()) {
		return nil, errs.New(errs.CodeForbidden, "租户已过期")
	}

	access, refresh, expiresIn, err := s.authM.Rotate(
		ctx, req.RefreshToken, u.ID, u.Username, t.ID, t.Code)
	if err != nil {
		return nil, err
	}
	return &types.TokenResp{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    expiresIn,
		Tenant:       types.TenantBrief{ID: types.ID(t.ID), Name: t.Name, Code: t.Code},
	}, nil
}

// Logout 登出（撤销当前会话）。
func (s *Auth) Logout(ctx context.Context, claims *auth.AccessClaims) {
	s.authM.Logout(ctx, claims)
}

// Info 用户信息 + 租户 + 角色 + 权限标识。
func (s *Auth) Info(ctx context.Context, claims *auth.AccessClaims) (*types.UserInfoResp, error) {
	u, err := dao.GetUserByID(ctx, claims.TenantID, claims.UserID)
	if err != nil {
		return nil, err
	}
	roles, err := dao.GetUserRoles(ctx, claims.TenantID, claims.UserID)
	if err != nil {
		return nil, err
	}
	menus, err := dao.GetUserMenus(ctx, claims.TenantID, claims.UserID, nil)
	if err != nil {
		return nil, err
	}

	roleCodes := make([]string, 0, len(roles))
	for _, r := range roles {
		roleCodes = append(roleCodes, r.Code)
	}
	perms := make([]string, 0, len(menus))
	permSet := make(map[string]struct{}, len(menus))
	for _, m := range menus {
		if m.Permission != "" {
			if _, dup := permSet[m.Permission]; !dup {
				permSet[m.Permission] = struct{}{}
				perms = append(perms, m.Permission)
			}
		}
	}

	dto := types.UserDTO{
		ID:        types.ID(u.ID),
		Username:  u.Username,
		Nickname:  u.Nickname,
		Email:     u.Email,
		Mobile:    derefString(u.Mobile),
		Sex:       u.Sex,
		Avatar:    u.Avatar,
		DeptID:    types.ID(u.DeptID),
		PostIDs:   parsePostIDs(u.PostIDs),
		Remark:    u.Remark,
		LoginIP:   u.LoginIP,
	}
	if u.LoginDate != nil {
		dto.LoginDate = *u.LoginDate
	}

	return &types.UserInfoResp{
		User:        dto,
		Tenant:      types.TenantBrief{ID: types.ID(claims.TenantID), Code: claims.TenantCode},
		Roles:       roleCodes,
		Permissions: perms,
	}, nil
}

// Routers 前端路由树（本租户可见菜单 type 1/2）。
func (s *Auth) Routers(ctx context.Context, claims *auth.AccessClaims) ([]types.RouterNode, error) {
	menus, err := dao.GetUserMenus(ctx, claims.TenantID, claims.UserID, []int8{1, 2})
	if err != nil {
		return nil, err
	}

	byID := make(map[int64]*types.RouterNode, len(menus))
	roots := make([]*types.RouterNode, 0)
	for _, m := range menus {
		node := &types.RouterNode{
			ID:            types.ID(m.ID),
			Name:          m.Name,
			Path:          m.Path,
			Component:     m.Component,
			ComponentName: m.ComponentName,
			Icon:          m.Icon,
			Sort:          m.Sort,
			Visible:       m.Visible,
			KeepAlive:     m.KeepAlive,
			AlwaysShow:    m.AlwaysShow,
		}
		byID[m.ID] = node
	}
	// 组树：parent 不在可见集合（或为根）→ 根节点
	for _, m := range menus {
		node := byID[m.ID]
		if parent, ok := byID[m.ParentID]; ok && m.ParentID != m.ID {
			parent.Children = append(parent.Children, *node)
		} else {
			roots = append(roots, node)
		}
	}
	return derefNodes(roots), nil
}

// UpdateProfile 个人信息更新（仅本人可改的字段）。
func (s *Auth) UpdateProfile(ctx context.Context, claims *auth.AccessClaims, req types.UpdateProfileReq) error {
	updates := map[string]any{
		"nickname": req.Nickname,
		"updater":  claims.Username,
	}
	if req.Email != "" {
		updates["email"] = req.Email
	}
	if req.Mobile != "" {
		updates["mobile"] = req.Mobile
	} else {
		updates["mobile"] = nil // 清空走 NULL，保 uk_tenant_mobile 多空值不冲突
	}
	if req.Sex != 0 {
		updates["sex"] = req.Sex
	}
	if req.Avatar != "" {
		updates["avatar"] = req.Avatar
	}

	res := tenantdb.DB(ctx).Model(&model.User{}).
		Where("tenant_id = ? AND id = ?", claims.TenantID, claims.UserID).
		Updates(updates)
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新个人信息失败")
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "用户不存在")
	}
	return nil
}

// UpdatePassword 改密：校验旧密 → 更新 → 撤销全部会话（踢下线，重新登录）。
func (s *Auth) UpdatePassword(ctx context.Context, claims *auth.AccessClaims, req types.UpdatePasswordReq) error {
	u, err := dao.GetUserByID(ctx, claims.TenantID, claims.UserID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.OldPassword)) != nil {
		return errs.New(errs.CodeParam, "原密码错误")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 10)
	if err != nil {
		return errs.Wrap(err, errs.CodeInternal, "密码加密失败")
	}
	res := tenantdb.DB(ctx).Model(&model.User{}).
		Where("tenant_id = ? AND id = ?", claims.TenantID, claims.UserID).
		Updates(map[string]any{"password": string(hash), "updater": claims.Username})
	if res.Error != nil {
		return errs.Wrap(res.Error, errs.CodeInternal, "更新密码失败")
	}

	s.authM.RevokeAllSessions(ctx, claims.TenantID, claims.UserID)
	return nil
}

// derefString 安全解引用。
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// parsePostIDs 解析 post_ids JSON 数组。
func parsePostIDs(s string) []types.ID {
	if s == "" {
		return []types.ID{}
	}
	var ids []types.ID
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return []types.ID{}
	}
	return ids
}

// derefNodes 指针树转值树（ Children 已为值切片）。
func derefNodes(nodes []*types.RouterNode) []types.RouterNode {
	out := make([]types.RouterNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, *n)
	}
	return out
}
