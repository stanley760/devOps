package authz

import "fmt"

// 初始策略种子（幂等）：角色名取自 RoleConfig，规则形状与历史版本逐字同形。
//
//	SeedBootstrap:  p,  {pa}, *,          *      平台管理员跨域全通（g2 路径）
//	                g2, {subject}, {pa}           绑定平台管理员
//	                p,  {sa}, {domain},   *      租户超管域内全通
//	                g,  {subject}, {sa},  {domain}
//
//	SeedTenantAdmin: p, {sa}, {domain},   *      租户超管域内全通
//	                 g, {subject}, {sa},  {domain}

// SeedBootstrap 初始策略（幂等，migrate/首次启动复用）。
// subject 为初始管理员主键（string 形态由调用方转换），domain 为租户域。
func (a *CasbinAuthorizer) SeedBootstrap(subject, domain string) error {
	if _, err := a.getEnforcer(); err != nil {
		return err
	}

	pa, sa := a.roles.PlatformAdmin, a.roles.SuperAdmin
	for _, rule := range []struct {
		ptype string
		vals  []string
	}{
		{"p", []string{pa, "*", "*"}},
		{"g2", []string{subject, pa}},
		{"p", []string{sa, domain, "*"}},
		{"g", []string{subject, sa, domain}},
	} {
		if err := a.AddPolicyIfAbsent(rule.ptype, rule.vals...); err != nil {
			return fmt.Errorf("写入种子 %s 行失败: %w", rule.ptype, err)
		}
	}
	return nil
}

// SeedTenantAdmin 租户初始超管策略（创建租户时调用，幂等）。
func (a *CasbinAuthorizer) SeedTenantAdmin(subject, domain string) error {
	if _, err := a.getEnforcer(); err != nil {
		return err
	}

	sa := a.roles.SuperAdmin
	if err := a.AddPolicyIfAbsent("p", sa, domain, "*"); err != nil {
		return fmt.Errorf("写入租户超管策略失败: %w", err)
	}
	if err := a.AddPolicyIfAbsent("g", subject, sa, domain); err != nil {
		return fmt.Errorf("写入租户超管绑定失败: %w", err)
	}
	return nil
}
