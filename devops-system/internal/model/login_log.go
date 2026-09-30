// t_system_login_log 登录审计表（租户表）。每次登录尝试（成功与失败）各记一行，
// 供审计追溯；由 service.Auth.login 的 defer 落库（尽力而为，不影响登录结果）。
// 租户校验失败的尝试无租户 ctx（TenantPlugin fail-closed 拒绝落库），
// 仅在访问日志与 auth_login 指标可见——审计表只覆盖租户已解析的路径。
package model

// LoginLog 登录审计。
type LoginLog struct {
	BaseTenantModel
	Username    string `gorm:"size:30;not null;index"`   // 尝试登录的用户名（失败也记录）
	UserID      int64  `gorm:"not null;default:0;index"`  // 用户命中后填；未知用户为 0
	IP          string `gorm:"size:50;not null;index"`    // 客户端真实 IP（XFF 链 + trusted_proxies 解析）
	UserAgent   string `gorm:"size:512"`                  // 登录端 UA
	GeoCountry  string `gorm:"size:64"`                   // 以下四项来自 geo-ip 插件注入的 X-Higress-Geo-* 头
	GeoProvince string `gorm:"size:64"`
	GeoCity     string `gorm:"size:64"`
	GeoIsp      string `gorm:"size:64"`
	Result      int8   `gorm:"not null;default:0"`        // 0成功 1失败
	Code        int    `gorm:"not null;default:200"`      // 业务码（同 auth_login 指标的 code 标签）
}

// TableName 表名。
func (LoginLog) TableName() string { return "t_system_login_log" }
