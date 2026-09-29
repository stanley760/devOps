// 租户管理服务：CRUD + 启停/过期 + 租户初始超管。
//
// 平台路由 ctx 无租户：t_tenant 为全局表可直接访问；创建租户初始超管时
// 显式注入新租户 ctx（tenantdb.With），user/role 行由 TenantPlugin 填充 tenant_id。
//
// 停用语义：状态落库 + 撤销租户全部会话（RevokeTenantSessions，
// 全员 access 立即 401）+ 清除 Tenant 中间件的 Redis 状态缓存（防 5min 缓存误放行）。
package service

import (
	"fmt"
	"context"
	"golang.org/x/crypto/bcrypt"
	"devops-infra/errs"
	"devops-system/internal/auth"
	"devops-system/internal/authz"
	"devops-system/internal/config"
	"devops-system/internal/dao"
	"devops-system/internal/model"
	"devops-system/internal/store"
	"devops-system/internal/tenantdb"
	"devops-system/internal/types"
	infratenant "devops-infra/tenant"
)

// Tenant 租户管理服务。
type Tenant struct {
	cfg   config.Config
	authM *auth.Manager
}

// NewTenant 创建服务。
func NewTenant(cfg config.Config, authM *auth.Manager) *Tenant {
	return &Tenant{cfg: cfg, authM: authM}
}

// List 分页查询。
func (s *Tenant) List(ctx context.Context, q types.TenantPageQuery) ([]types.TenantDTO, int64, error) {
	tenants, total, err := dao.ListTenants(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]types.TenantDTO, 0, len(tenants))
	for i := range tenants {
		out = append(out, toTenantDTO(&tenants[i]))
	}
	return out, total, nil
}

// Get 租户详情。
func (s *Tenant) Get(ctx context.Context, id types.ID) (*types.TenantDTO, error) {
	t, err := dao.GetTenantByID(ctx, id.Int64())
	if err != nil {
		return nil, err
	}
	dto := toTenantDTO(t)
	return &dto, nil
}

// Create 创建租户 + 租户初始超管（super_admin 角色 + casbin 域内全通 p/g 行）。
// 模式B：先建库迁移（provision），数据写入走租户库连接（tenantdb resolver）。
func (s *Tenant) Create(ctx context.Context, claims *auth.AccessClaims, req types.TenantCreateReq) (*types.TenantDTO, error) {
	exists, err := dao.TenantCodeExists(ctx, req.Code, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errs.New(errs.CodeParam, "租户编码已存在")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), 10)
	if err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "密码加密失败")
	}

	t := &model.Tenant{
		BaseModel:     model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		Name:          req.Name,
		Code:          req.Code,
		ContactName:   req.ContactName,
		ContactMobile: req.ContactMobile,
		ExpireTime:    req.ExpireTime,
		Remark:        req.Remark,
		Status:        0,
	}
	if err := dao.CreateTenant(ctx, t); err != nil {
		return nil, err
	}

	// 模式B：为新租户建库 + AutoMigrate（数据随后经 resolver 写入租户库）
	if s.cfg.Tenant.Mode == "schema" {
		if err := tenantdb.ProvisionSchema(ctx, t.Code); err != nil {
			_ = dao.HardDeleteTenant(ctx, t.ID)
			return nil, err
		}
	}

	// 租户初始超管：注入新租户 ctx（平台路由 ctx 无租户；模式B 据此路由到租户库）
	tctx := infratenant.With(ctx, t.ID, t.Code)
	admin := &model.User{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		},
		Username: req.AdminUsername,
		Password: string(hash),
		Nickname: req.AdminUsername,
		Status:   0,
	}
	role := &model.Role{
		BaseTenantModel: model.BaseTenantModel{
			BaseModel: model.BaseModel{Creator: claims.Username, Updater: claims.Username},
		},
		Name: "超级管理员", Code: "super_admin", Type: 1, DataScope: 1, Status: 0,
	}
	if err := dao.SeedTenantAdmin(tctx, role, admin); err != nil {
		// 补偿：回滚租户行，避免「有租户无超管」的中间态（模式B 库保留，migrate 幂等可复用）
		_ = dao.HardDeleteTenant(ctx, t.ID)
		return nil, err
	}

	if err := authz.SeedTenantAdmin(t.ID, admin.ID); err != nil {
		return nil, errs.Wrap(err, errs.CodeInternal, "租户已创建，但超管授权写入失败，请重试")
	}
	dto := toTenantDTO(t)
	return &dto, nil
}

// Update 更新租户（name/contact/expire_time/remark；code 不可改）。
func (s *Tenant) Update(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.TenantUpdateReq) error {
	t, err := dao.GetTenantByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	updates := map[string]any{
		"name":           req.Name,
		"contact_name":   req.ContactName,
		"contact_mobile": req.ContactMobile,
		"remark":         req.Remark,
		"updater":        claims.Username,
	}
	if req.ExpireTime != nil {
		updates["expire_time"] = *req.ExpireTime
	} else {
		updates["expire_time"] = nil // 永不过期
	}
	return dao.UpdateTenant(ctx, t.ID, updates)
}

// UpdateStatus 启停租户：停用即全员下线（会话全量撤销 + 状态缓存清除）。
func (s *Tenant) UpdateStatus(ctx context.Context, claims *auth.AccessClaims, id types.ID, req types.TenantStatusReq) error {
	t, err := dao.GetTenantByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	if t.ID == 1 && req.Status != 0 {
		return errs.New(errs.CodeParam, "默认租户不可停用")
	}
	if t.Status == req.Status {
		return nil // 幂等：状态未变直接成功
	}

	if err := dao.UpdateTenant(ctx, t.ID, map[string]any{
		"status":  req.Status,
		"updater": claims.Username,
	}); err != nil {
		return err
	}

	// 清除 Tenant 中间件状态缓存（停用防 5min 缓存误放行；启用防读到旧停用态）
	if c := store.Cache(); c != nil {
		_ = c.Delete(ctx, fmt.Sprintf("tenant:%d", t.ID))
	}
	if req.Status == 1 {
		s.authM.RevokeTenantSessions(ctx, t.ID)
	}
	return nil
}

// Delete 删除租户：默认租户不可删；未停用不可删；模式A 逻辑删（数据保留）。
func (s *Tenant) Delete(ctx context.Context, claims *auth.AccessClaims, id types.ID) error {
	t, err := dao.GetTenantByID(ctx, id.Int64())
	if err != nil {
		return err
	}
	if t.ID == 1 {
		return errs.New(errs.CodeParam, "默认租户不可删除")
	}
	if t.Status == 0 {
		return errs.New(errs.CodeParam, "租户未停用，请先停用再删除")
	}

	if err := dao.SoftDeleteTenant(ctx, t.ID, t.Code); err != nil {
		return err
	}

	// 保险：清残余会话与状态缓存（停用时已撤，此处兜底）
	s.authM.RevokeTenantSessions(ctx, t.ID)
	if c := store.Cache(); c != nil {
		_ = c.Delete(ctx, fmt.Sprintf("tenant:%d", t.ID))
	}
	// 模式A：casbin g/p 行与业务数据保留（租户已删不可登录），清理工具列 Roadmap
	return nil
}

// toTenantDTO model.Tenant → DTO。
func toTenantDTO(t *model.Tenant) types.TenantDTO {
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
