// 登录审计查询模块 DTO（管理端 t_system_login_log 读取侧；写入侧见 service.Auth.login）。
package types

import "time"

// LoginLogPageQuery GET /login-logs 查询参数。
type LoginLogPageQuery struct {
	PageNum   int
	PageSize  int
	Username  string     // 用户名模糊匹配（失败尝试也记录）
	IP        string     // 客户端 IP 模糊匹配（支持网段前缀，如 "192.168."）
	Result    *int8      // 0成功 1失败；nil 不过滤
	BeginTime *time.Time // create_time 下界（含）
	EndTime   *time.Time // create_time 上界（含）
}

// LoginLogDTO 登录审计行。
type LoginLogDTO struct {
	ID          ID        `json:"id"`
	Username    string    `json:"username"`
	UserID      ID        `json:"userId"` // 未知用户为 0
	IP          string    `json:"ip"`
	UserAgent   string    `json:"userAgent"`
	GeoCountry  string    `json:"geoCountry"` // 以下四项来自 higress geo-ip 插件注入头
	GeoProvince string    `json:"geoProvince"`
	GeoCity     string    `json:"geoCity"`
	GeoIsp      string    `json:"geoIsp"`
	Result      int8      `json:"result"` // 0成功 1失败
	Code        int       `json:"code"`   // 业务码（同 auth_login 指标 code 标签）
	CreateTime  time.Time `json:"createTime"`
}
