// 操作审计查询模块 DTO（管理端 t_system_operation_log 读取侧；
// 写入侧由 humakit 审计采集中间件经 audit.Sink 自动落库，无对应写入接口）。
package types

import (
	"time"

	commonaudit "github.com/stanley760/devops/devops-common/audit"
	"github.com/stanley760/devops/devops-common/domain"
)

// OperationLogPageQuery GET /operation-logs 查询参数。
type OperationLogPageQuery struct {
	domain.PageParam
	Module    string     `doc:"一级模块，精确匹配，不传为不过滤"`
	SubModule string     `doc:"二级模块，精确匹配，不传为不过滤"`
	UserName  string     `doc:"操作者名称，模糊匹配"`
	UserIP    string     `doc:"客户端 IP，模糊匹配"`
	Result    *int8      `doc:"执行结果 0:成功；1:失败，不传为不过滤"`
	BeginTime *time.Time `doc:"开始时间（含），create_time 下界"`
	EndTime   *time.Time `doc:"结束时间（含），create_time 上界"`
}

// OperationLogDTO 操作审计行。
type OperationLogDTO struct {
	ID            ID                   `json:"id" doc:"记录id"`
	TraceID       string               `json:"traceId" doc:"链路追踪编号"`
	UserID        ID                   `json:"userId" doc:"操作者用户id，未知为 0"`
	UserName      string               `json:"userName" doc:"操作者名称"`
	UserType      commonaudit.UserType `json:"userType" doc:"操作者类别 1:会员(C端)；2:管理员(B端)"`
	UserTypeName  string               `json:"userTypeName" doc:"操作者类别名称"`
	Module        string               `json:"module" doc:"一级模块"`
	SubModule     string               `json:"subModule" doc:"二级模块"`
	BizID         ID                   `json:"bizId" doc:"关联业务主键"`
	Contents      string               `json:"contents" doc:"变更内容摘要"`
	Extra         string               `json:"extra" doc:"扩展信息（JSON），如耗时"`
	RequestMethod string               `json:"requestMethod" doc:"HTTP 方法"`
	RequestURL    string               `json:"requestUrl" doc:"请求路径"`
	UserIP        string               `json:"userIp" doc:"客户端 IP"`
	UserAgent     string               `json:"userAgent" doc:"客户端 User-Agent"`
	GeoCountry    string               `json:"geoCountry" doc:"国家（higress geo-ip 插件注入头）"`
	GeoProvince   string               `json:"geoProvince" doc:"省份（higress geo-ip 插件注入头）"`
	GeoCity       string               `json:"geoCity" doc:"城市（higress geo-ip 插件注入头）"`
	GeoIsp        string               `json:"geoIsp" doc:"运营商（higress geo-ip 插件注入头）"`
	Result        int8                 `json:"result" doc:"执行结果 0:成功；1:失败"`
	Code          int                  `json:"code" doc:"HTTP 状态码，与响应信封一致"`
	CreateTime    time.Time            `json:"createTime" doc:"操作时间"`
}
