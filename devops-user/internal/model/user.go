// Package model 定义 devops-user 的表模型。
//
// 与 devops-system 的差异：
//   - 无 tenant_id：C 端单租户，不做多租户隔离（故也没有 TenantPlugin）；
//   - 无 role/dept：权限靠「只能操作自己的数据」，由 service 按 claims.UID 过滤；
//   - BaseModel 上提到 devops-common/model 复用，此处只声明业务字段。
//
// 发号与审计操作者由装配期注入（见 cmd/root.go 的 commonmodel.Configure）：
// 本服务注册时无认证操作者，故 ActorResolver 传 nil，creator/updater 固定为 system。
package model

import (
	"strings"

	commonmodel "github.com/stanley760/devops/devops-common/model"
	"github.com/stanley760/devops/devops-id/pkg/idgorm"
)

// idHook 进程级发号钩子：把 Generator 适配为 commonmodel.IDAssigner
// （真正带 Assign 方法的是 *idgorm.Hook，而非 Generator）。
var idHook = idgorm.New(nil, nil)

// BaseModel 上提至 devops-common/model，两个服务共用同一份实现
// （主键发号 + 审计字段 + 软删）。此处做类型别名，便于本包内引用。
type BaseModel = commonmodel.BaseModel

// SetIDGenerator 注入发号客户端（server 命令装配期调用）。
//
// ActorResolver 固定为 nil：注册时用户尚未登录，C 端不存在「认证操作者」概念，
// creator/updater 恒为 commonmodel.ActorFallback（system）。
// 不读 auditctx —— 那是 B 端 admin 语义，C 端照搬会把后台用户名写进 C 端审计字段。
func SetIDGenerator(generator idgorm.Generator) {
	idHook.SetGenerator(generator)
	commonmodel.Configure(idHook, nil)
}

// ConfigureModel 装配共用基模型（C 端语义：同 SetIDGenerator）。
//
// 单独提供是为了与 devops-system 的装配入口同名对齐，装配处可读性更好。
func ConfigureModel(generator idgorm.Generator) {
	SetIDGenerator(generator)
}

// User C 端用户（t_user 表）。
//
// 唯一性：uk_username(username) —— C 端无租户维度，用户名全局唯一。
// 该唯一索引由 dbinit/sql/schema 建表 SQL 声明（GORM tag 无法跨嵌入组合）。
type User struct {
	BaseModel
	Username string `gorm:"size:32;not null" json:"username" doc:"登录名"`
	Password string `gorm:"size:100;not null" json:"-" doc:"口令哈希（永不返回）"`
	Nickname string `gorm:"size:32;not null" json:"nickname" doc:"昵称"`
	// Email / Mobile / Avatar 用指针：未填写时存 NULL。
	// MySQL 唯一索引不约束多个 NULL，故必须用 NULL 而非空串表示「未填写」——
	// 否则第二个未绑定邮箱的用户会与第一个的空串冲突。
	Email  *string `gorm:"size:100;uniqueIndex:uk_email" json:"email" doc:"邮箱"`
	Mobile *string `gorm:"size:20;uniqueIndex:uk_mobile" json:"mobile" doc:"手机号"`
	Avatar *string `gorm:"size:512" json:"avatar" doc:"头像地址"`

	Disabled bool `gorm:"not null;default:false" json:"disabled" doc:"是否已封禁"`
}

// TableName 表名。
func (User) TableName() string { return "t_user" }

// PublicProfile 对外暴露的资料视图（不含口令等敏感字段）。
type PublicProfile struct {
	ID        int64  `json:"id" doc:"用户 ID"`
	Username  string `json:"username" doc:"登录名"`
	Nickname  string `json:"nickname" doc:"昵称"`
	Email     string `json:"email" doc:"邮箱"`
	Mobile    string `json:"mobile" doc:"手机号"`
	Avatar    string `json:"avatar" doc:"头像地址"`
	CreatedAt int64  `json:"created_at" doc:"注册时间（Unix 毫秒）"`
}

// ToPublic 脱敏转换为对外视图；指针字段的 nil 归一化为空串，
// 避免 JSON 里出现 null 影响前端渲染。
func (u *User) ToPublic() PublicProfile {
	if u == nil {
		return PublicProfile{}
	}
	return PublicProfile{
		ID:        u.ID,
		Username:  u.Username,
		Nickname:  u.Nickname,
		Email:     deref(u.Email),
		Mobile:    deref(u.Mobile),
		Avatar:    deref(u.Avatar),
		CreatedAt: u.CreateTime.UnixMilli(),
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// Normalize 归一化输入：去空白、nil 归一化为空串（写库前调用）。
func (u *User) Normalize() {
	u.Username = strings.TrimSpace(u.Username)
	u.Nickname = strings.TrimSpace(u.Nickname)
	u.Email = NormalizePtr(u.Email)
	u.Mobile = NormalizePtr(u.Mobile)
	u.Avatar = NormalizePtr(u.Avatar)
	if u.Nickname == "" {
		u.Nickname = u.Username
	}
}

// NormalizePtr 空串归一化为 nil：让唯一索引下的「未填写」统一存 NULL，
// 否则第二个未填邮箱的用户会与第一个空串冲突。
//
// 导出供 service 的部分更新使用（dao.UpdateProfile 走 map 更新，不再经User.Normalize）。
func NormalizePtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}
