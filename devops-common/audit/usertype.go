// Package audit 定义跨业务复用的操作审计契约。
//
// 本包保持零外部依赖（仅标准库），只描述"记录什么"与"写到哪里"，
// 不关心持久化实现：本地库、远端 RPC、消息队列均可由 Sink 实现方决定。
//
// 审计是旁路能力，任何写入失败都不得影响业务主链路；
// 列宽截断与降级语义由实现方遵循本包常量与 Sink 注释。
package audit

import "strconv"

// UserType 操作者类别：区分 C 端会员与 B 端管理员。
// 独立于具体业务模块，任何需要区分操作者来源的审计场景共用此枚举。
type UserType int64

const (
	// UserTypeMember 面向 C 端的普通用户（会员）。
	UserTypeMember UserType = 1
	// UserTypeAdmin 面向 B 端的管理后台用户（管理员）。
	UserTypeAdmin UserType = 2
)

// userTypeLabels 枚举中文名，与业务枚举文案对齐。
var userTypeLabels = map[UserType]string{
	UserTypeMember: "会员",
	UserTypeAdmin:  "管理员",
}

// Valid 判断是否为已定义的枚举值。
func (u UserType) Valid() bool {
	_, ok := userTypeLabels[u]
	return ok
}

// String 返回枚举中文名；未定义值回退为「未知(<数值>)」。
func (u UserType) String() string {
	if label, ok := userTypeLabels[u]; ok {
		return label
	}
	return "未知(" + strconv.FormatInt(int64(u), 10) + ")"
}

// UserTypes 返回全部枚举值，按数值升序，供枚举文档/校验遍历使用。
func UserTypes() []UserType {
	return []UserType{UserTypeMember, UserTypeAdmin}
}