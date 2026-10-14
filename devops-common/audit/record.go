package audit

import "context"

// Record 一条操作审计记录。
//
// 零外部依赖，不含 gorm tag：持久化映射由实现方（如 devops-infra/audit 的
// GORM Sink）按各业务表结构自行完成，契约层不绑定具体存储。
type Record struct {
	// TenantID 租户ID；全局表操作时为 0。
	TenantID int64
	// TraceID 链路追踪编号，用于与日志/链路系统互查。
	TraceID string
	// UserID 操作者主键；未知或系统写入时为 0。
	UserID int64
	// Username 操作者名称（登录名）。
	Username string
	// UserType 操作者类别（C 端会员 / B 端管理员）。
	UserType UserType

	// Module 一级模块，如 system。
	Module string
	// SubModule 二级模块，如 user。
	SubModule string
	// BizID 业务主键，如被改动的用户 ID；无关联实体时为 0。
	BizID int64

	// Contents 变更内容摘要；一期通常留空或仅记请求体摘要。
	Contents string
	// Extra 扩展信息（JSON 文本），如耗时、结果码。
	Extra string

	// RequestMethod HTTP 方法。
	RequestMethod string
	// RequestURL 请求路径。
	RequestURL string
	// UserIP 客户端真实 IP。
	UserIP string
	// UserAgent 客户端 UA。
	UserAgent string
	// 以下四项为客户端归属地，来自网关 geo-ip 插件注入的 X-Higress-Geo-* 头。
	GeoCountry  string
	GeoProvince string
	GeoCity     string
	GeoISP      string

	// Result 执行结果：0 成功，1 失败。
	Result int8
	// Code 业务码，与响应信封一致（成功 200）。
	Code int
}

// 审计结果取值。
const (
	ResultSuccess int8 = 0
	ResultFailure int8 = 1
)

// ClientMeta 客户端元数据，由 HTTP 层从请求中提取后随审计上报。
// Geo 四项来自网关 geo-ip 插件注入的 X-Higress-Geo-* 头。
type ClientMeta struct {
	IP          string
	UserAgent   string
	GeoCountry  string
	GeoProvince string
	GeoCity     string
	GeoISP      string
}

// Sink 审计写入端。
//
// 实现约定（所有实现方必须遵守）：
//   - Emit 是旁路调用，绝不可阻塞或失败影响业务主链路
//   - 写入失败仅记录日志与指标，由实现方兜底
//   - 实现方需按本包列宽常量截断超长字段，保行不丢
type Sink interface {
	Emit(ctx context.Context, rec Record)
}

// SinkFunc 把普通函数适配为 Sink，便于测试与轻量注入。
type SinkFunc func(ctx context.Context, rec Record)

// Emit 实现 Sink。
func (f SinkFunc) Emit(ctx context.Context, rec Record) { f(ctx, rec) }

// Noop 是丢弃全部记录的 Sink，用作未装配时的安全默认值，
// 避免业务代码到处判nil。
var Noop Sink = SinkFunc(func(context.Context, Record) {})

// 审计字段列宽上限（按字符计，与各业务表 gorm tag 对齐）。
//
// 这些值是契约的一部分：实现方截断时以此为准，业务表 DDL 应与之保持一致，
// 否则超长内容会在严格模式下导致整行插入失败（丢审计行比截断更不可接受）。
const (
	MaxUsername  = 30
	MaxIP        = 50
	MaxUserAgent = 512
	MaxGeo       = 64
	MaxModule    = 50
	MaxSubModule = 50
	MaxURL       = 255
	MaxTraceID   = 100
	MaxMethod    = 16
)

// Clip 按字符数截断字符串到 n。
//
// MySQL varchar 按字符计长；按字节截会截出非法 UTF-8，中文归属地、
// 中文用户名正是高风险场景。len(s) 字节数不超 n 时必不超 n 字符，快路径直接返回。
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ClipClientMeta 按列宽截断客户端元数据中的全部字段。
func ClipClientMeta(m ClientMeta) ClientMeta {
	m.IP = Clip(m.IP, MaxIP)
	m.UserAgent = Clip(m.UserAgent, MaxUserAgent)
	m.GeoCountry = Clip(m.GeoCountry, MaxGeo)
	m.GeoProvince = Clip(m.GeoProvince, MaxGeo)
	m.GeoCity = Clip(m.GeoCity, MaxGeo)
	m.GeoISP = Clip(m.GeoISP, MaxGeo)
	return m
}

// Normalize 按契约列宽截断记录中的全部受限字段。
//
// UserType 不做改写：未定义枚举值保留原值便于落库后排查，
// 由消费方（查询侧）决定如何呈现。
func (r Record) Normalize() Record {
	r.TraceID = Clip(r.TraceID, MaxTraceID)
	r.Username = Clip(r.Username, MaxUsername)
	r.UserIP = Clip(r.UserIP, MaxIP)
	r.UserAgent = Clip(r.UserAgent, MaxUserAgent)
	r.GeoCountry = Clip(r.GeoCountry, MaxGeo)
	r.GeoProvince = Clip(r.GeoProvince, MaxGeo)
	r.GeoCity = Clip(r.GeoCity, MaxGeo)
	r.GeoISP = Clip(r.GeoISP, MaxGeo)
	r.Module = Clip(r.Module, MaxModule)
	r.SubModule = Clip(r.SubModule, MaxSubModule)
	r.RequestMethod = Clip(r.RequestMethod, MaxMethod)
	r.RequestURL = Clip(r.RequestURL, MaxURL)
	return r
}