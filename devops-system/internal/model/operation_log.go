package model

import (
	commonaudit "github.com/stanley760/devOps/devops-common/audit"
)

// OperationLog 操作审计表（租户表）。
//
// 由 humakit 审计采集中间件经 audit.Sink 写入（契约见 devops-common/audit），
// 列宽与契约常量一一对应；查询/删除/清空走管理端
// /system/api/v1/operation-logs（system:operationlog:* 权限门控）。
//
// 与登录审计（LoginLog）的分工：LoginLog 记录"能否登录"这一条认证链路的
// 尝试与结果；OperationLog 记录"B 端管理操作做了什么"，覆盖用户/角色/菜单/
// 部门/岗位/租户的写操作。
type OperationLog struct {
	BaseTenantModel

	// 链路与操作者
	TraceID   string               `gorm:"column:trace_id;size:100;not null;default:'';index"` // 链路追踪编号，可与日志/链路系统互查
	UserID    int64                `gorm:"not null;default:0;index"`                          // 操作者用户编号，未知为 0
	UserName  string               `gorm:"size:30;not null;default:'';index"`                  // 操作者名称（快照，避免改名后无法追溯）
	UserType  commonaudit.UserType `gorm:"not null;default:0"`                                // 操作者类别：1 会员(C端) / 2 管理员(B端)

	// 操作定位
	Module    string `gorm:"size:50;not null;default:'';index"` // 一级模块，如 system
	SubModule string `gorm:"size:50;not null;default:'';index"` // 二级模块，如 user
	BizID     int64  `gorm:"column:biz_id;not null;default:0"`   // 业务主键，如被改动的用户 ID

	// 内容与来源
	Contents      string `gorm:"type:text"`                    // 变更内容摘要；一期通常为空
	Extra         string `gorm:"type:text"`                    // 扩展信息（JSON），如耗时
	RequestMethod string `gorm:"size:16;not null;default:''"`  //
	RequestURL    string `gorm:"size:255;not null;default:''"` //
	UserIP        string `gorm:"size:50;not null;default:'';index"`
	UserAgent     string `gorm:"size:512;not null;default:''"`
	GeoCountry    string `gorm:"size:64;not null;default:''"`
	GeoProvince   string `gorm:"size:64;not null;default:''"`
	GeoCity       string `gorm:"size:64;not null;default:''"`
	GeoISP        string `gorm:"size:64;not null;default:''"`

	// 结果
	Result int8 `gorm:"not null;default:0"` // 0 成功 1 失败
	Code   int  `gorm:"not null;default:0"` // HTTP 状态码，与响应信封一致
}

// TableName 表名。
func (OperationLog) TableName() string { return "t_system_operation_log" }

// FromRecord 把审计契约记录映射为本表实体。
//
// 供 devops-infra/audit 的 GORSink 使用：契约层（devops-common/audit）不
// 感知 gorm tag 与表结构，落库映射由业务表侧完成。
func FromRecord(rec commonaudit.Record) *OperationLog {
	return &OperationLog{
		TenantID:      rec.TenantID,
		TraceID:       rec.TraceID,
		UserID:        rec.UserID,
		UserName:      rec.Username,
		UserType:      rec.UserType,
		Module:        rec.Module,
		SubModule:     rec.SubModule,
		BizID:         rec.BizID,
		Contents:      rec.Contents,
		Extra:         rec.Extra,
		RequestMethod: rec.RequestMethod,
		RequestURL:    rec.RequestURL,
		UserIP:        rec.UserIP,
		UserAgent:     rec.UserAgent,
		GeoCountry:    rec.GeoCountry,
		GeoProvince:   rec.GeoProvince,
		GeoCity:       rec.GeoCity,
		GeoISP:        rec.GeoISP,
		Result:        rec.Result,
		Code:          rec.Code,
	}
}