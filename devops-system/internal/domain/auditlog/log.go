// Package auditlog 是审计日志限界上下文：登录日志与操作日志。
//
// 两者都是只追加（append-only）的记录型数据，无聚合间引用、无业务不变量，
// 因此以「记录」而非「聚合根」建模——这是 DDD 中对简单模型的正确取舍：
// 不为了形式上的聚合而给日志强加不存在的生命周期。
package auditlog

import (
	"context"
	"strings"
	"time"

	"github.com/stanley760/devOps/devops-common/domain"

	"github.com/stanley760/devOps/devops-system/internal/domain/iam"
)

// 登录结果（与既有 DB 列口径一致）。
const (
	LoginResultSuccess int8 = 0
	LoginResultFailure int8 = 1
)

// LoginLog 登录日志记录（t_system_login_log）。
//
// 字段名与列名对齐：result（0 成功 1 失败）、code（业务码）。
// geo_* 四项来自 geo-ip 插件注入的 X-Higress-Geo-* 头。
type LoginLog struct {
	ID       int64
	TenantID int64
	Username string
	// UserID 用户命中后填；未知用户为 0。
	UserID      int64
	IP          string
	UserAgent   string
	GeoCountry  string
	GeoProvince string
	GeoCity     string
	GeoISP      string
	Result      int8
	// Code 业务码（同 auth_login 指标的 code 标签）。
	Code int
	// CreateTime 即登录发生时间（表无独立 login_time 列，用基类审计时间）。
	CreateTime time.Time
}

// LoginLogFilter 登录日志查询条件（列名对齐 t_system_login_log）。
type LoginLogFilter struct {
	domain.PageParam
	Username  string
	IP        string
	Result    *int8
	BeginTime *string
	EndTime   *string
}

// OperationLog 操作日志记录（t_system_operation_log）。
//
// 字段名与列名对齐：user_name/user_ip/geo_isp（列名带 user/geo 前缀）、
// request_url/request_method、result（0 成功 1 失败）、code（HTTP 状态码）。
//
// 写入路径不在本服务：操作审计由 humakit 审计采集中间件经
// devops-infra/audit.Sink 落库（保持既有双轨制）。本领域模型仅承载
// 查询侧读模型与管理端删除/清空。
type OperationLog struct {
	ID       int64
	TenantID int64
	TraceID  string
	UserID   int64
	UserName string
	UserType int64
	Module   string
	// SubModule 二级模块，如 user。
	SubModule string
	// BizID 业务主键，如被改动的用户 ID。
	BizID         int64
	Contents      string
	Extra         string
	RequestMethod string
	RequestURL    string
	UserIP        string
	UserAgent     string
	GeoCountry    string
	GeoProvince   string
	GeoCity       string
	GeoISP        string
	Result        int8
	Code          int
	CreateTime    time.Time
}

// OperationLogFilter 操作日志查询条件（列名对齐 t_system_operation_log）。
type OperationLogFilter struct {
	domain.PageParam
	Module    string
	SubModule string
	UserName  string
	UserIP    string
	Result    *int8
	BeginTime *string
	EndTime   *string
}

// LoginLogWriter 登录日志写入端口（写侧）。
type LoginLogWriter interface {
	Append(ctx context.Context, l *LoginLog) error
}

// OperationLogWriter 操作日志写入端口（写侧）。
type OperationLogWriter interface {
	Append(ctx context.Context, l *OperationLog) error
}

// LoginLogReader 登录日志读端口。
type LoginLogReader interface {
	List(ctx context.Context, f LoginLogFilter) (domain.PageResult[*LoginLog], error)
	// DeleteByIDs 物理删除（审计日志清理属物理删，既有实现用 Unscoped）。
	DeleteByIDs(ctx context.Context, ids []int64) error
	// Clean 清空当前租户的登录日志。
	Clean(ctx context.Context) error
}

// OperationLogReader 操作日志读端口。
type OperationLogReader interface {
	List(ctx context.Context, f OperationLogFilter) (domain.PageResult[*OperationLog], error)
	DeleteByIDs(ctx context.Context, ids []int64) error
	Clean(ctx context.Context) error
}

// IDList 解析逗号分隔 ID 字符串为 ID 切片。
//
// 改造前 service/login_log.go 与 service/operation_log.go 各有一份逐字相同的
// parseIDsCSV，现收敛到领域层唯一实现。
func IDList(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ids = append(ids, iam.IDsFromStrings([]string{p})...)
	}
	return ids
}

// ClipRunes 按 rune 截断字符串（审计日志字段长度收口，防止超列截断）。
func ClipRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
