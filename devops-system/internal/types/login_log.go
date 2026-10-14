// 登录审计查询模块 DTO（管理端 t_system_login_log 读取侧；写入侧见 service.Auth.login）。
package types

import (
	"time"

	"github.com/stanley760/devops/devops-common/domain"
)

// LoginLogPageQuery GET /login-logs 查询参数。
type LoginLogPageQuery struct {
	domain.PageParam
	Username  string     `doc:"用户名，模糊匹配（失败尝试也记录）"`
	IP        string     `doc:"客户端 IP，模糊匹配（支持网段前缀，如 \"192.168.\"）"`
	Result    *int8      `doc:"登录结果 0:成功；1:失败，不传为不过滤"`
	BeginTime *time.Time `doc:"开始时间（含），create_time 下界"`
	EndTime   *time.Time `doc:"结束时间（含），create_time 上界"`
}

// LoginLogDTO 登录审计行。
type LoginLogDTO struct {
	ID          ID        `json:"id" doc:"记录id"`
	Username    string    `json:"username" doc:"用户名"`
	UserID      ID        `json:"userId" doc:"用户id，未知用户为 0"`
	IP          string    `json:"ip" doc:"客户端 IP"`
	UserAgent   string    `json:"userAgent" doc:"客户端 User-Agent"`
	GeoCountry  string    `json:"geoCountry" doc:"国家（higress geo-ip 插件注入头）"`
	GeoProvince string    `json:"geoProvince" doc:"省份（higress geo-ip 插件注入头）"`
	GeoCity     string    `json:"geoCity" doc:"城市（higress geo-ip 插件注入头）"`
	GeoIsp      string    `json:"geoIsp" doc:"运营商（higress geo-ip 插件注入头）"`
	Result      int8      `json:"result" doc:"登录结果 0:成功；1:失败"`
	Code        int       `json:"code" doc:"业务码，同 auth_login 指标 code 标签"`
	CreateTime  time.Time `json:"createTime" doc:"登录时间"`
}
