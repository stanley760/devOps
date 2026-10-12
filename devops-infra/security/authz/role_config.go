package authz

// 角色名配置：平台超管与租户超管的角色标识由业务注入（或走默认），
// 避免种子策略与 IsPlatformAdmin 判定各写死一份字符串导致脑裂。
const (
	defaultPlatformAdmin = "platform_admin"
	defaultSuperAdmin    = "super_admin"
)

// RoleConfig 角色名配置；零值字段回填默认，保证向后兼容（yudao 口径）。
type RoleConfig struct {
	PlatformAdmin string // 平台管理员角色（g2 二元组 + p,*,* 通配），默认 "platform_admin"
	SuperAdmin    string // 租户超管角色（p,{domain},* + g 三元组），默认 "super_admin"
}

// DefaultRoleConfig 默认角色名配置（与历史硬编码字符串一致）。
func DefaultRoleConfig() RoleConfig {
	return RoleConfig{
		PlatformAdmin: defaultPlatformAdmin,
		SuperAdmin:    defaultSuperAdmin,
	}
}

// withDefaults 零值字段回填默认，保证部分自定义也能跑。
func (c RoleConfig) withDefaults() RoleConfig {
	if c.PlatformAdmin == "" {
		c.PlatformAdmin = defaultPlatformAdmin
	}
	if c.SuperAdmin == "" {
		c.SuperAdmin = defaultSuperAdmin
	}
	return c
}
