package model

// OAuthProvider 三方登录提供方。
//
// 只列已接入的提供方；后续要加别的（如钉钉、飞书）在此扩展并补
// refreshtoken 的授权 URL 与用户信息解析分支。
type OAuthProvider string

const (
	// ProviderGithub GitHub OAuth（https://docs.github.com/apps/oauth-apps/）。
	ProviderGithub OAuthProvider = "github"
	// ProviderGoogle Google OAuth 2.0（OIDC）。
	ProviderGoogle OAuthProvider = "google"
	// ProviderWechat 微信开放平台「网站应用」扫码登录。
	ProviderWechat OAuthProvider = "wechat"
)

// Valid 判断是否为已支持的提供方。
func (p OAuthProvider) Valid() bool {
	switch p {
	case ProviderGithub, ProviderGoogle, ProviderWechat:
		return true
	}
	return false
}

// String 实现 Stringer。
func (p OAuthProvider) String() string { return string(p) }

// OAuthIdentity 三方账号绑定（t_user_oauth 表）。
//
// ## 唯一性
//
//	uk_provider_provider_id (provider, provider_user_id)  同一三方账号只能绑一个本地用户
//	uk_user_provider        (user_id, provider)         同一用户同一提供方只能绑一次
//
// 两个唯一索引缺一不可：前者防「一个三方账号绑多个本地用户」（账号劫持），
// 后者防「同一用户重复绑定同一提供方」导致数据混乱。
//
// ## 与本地账号的关系
//
// 三方绑定是**可选的登录方式**而非账号主体：user_id 指向 t_user.id，
// 该用户仍可���本地用户名口令登录（未绑定时亦可首次用三方登录自动建号）。
type OAuthIdentity struct {
	BaseModel
	UserID int64 `gorm:"not null;uniqueIndex:uk_user_provider" json:"user_id" doc:"本地用户 ID"`

	Provider OAuthProvider `gorm:"size:16;not null;uniqueIndex:uk_provider_provider_id,priority:1" json:"provider" doc:"三方提供方"`
	// ProviderUserID 是三方侧的稳定用户标识（github 的 numeric id、
	// google 的 sub、wechat 的 unionid）。**不用 username/email/nickname 做键**——
	// 用户名可改、邮箱可换、微信昵称可重复，用它们当键会导致绑定错乱或被冒用。
	//
	// ⚠️ 微信**没有 email 也不返回 nickname 唯一性保证**，故 Email 字段对纯微信用户恒为空。
	ProviderUserID string `gorm:"size:128;not null;uniqueIndex:uk_provider_provider_id,priority:2" json:"provider_user_id" doc:"三方用户唯一标识"`

	// 展示信息快照（登录时刷新，仅用于展示，非身份依据）。
	Nickname  string `gorm:"size:64" json:"nickname" doc:"三方昵称快照"`
	AvatarURL string `gorm:"size:512" json:"avatar_url" doc:"三方头像快照"`

	// 最近授权时拿到的 access/refresh token。
	//
	//⚠️ **高敏感凭据**：用于后续调用三方 API（如拉取邮箱、发布评论）。
	// 生产必须加密存储（应用层 AES-GCM，主密钥进密钥管理），不可明文入库。
	// 当前骨架期直接存明文并标注警告——上线前必须改。
	AccessToken  string `gorm:"size:512" json:"-" doc:"三方 access token（高敏感，上线前须加密）"`
	RefreshToken string `gorm:"size:512" json:"-" doc:"三方 refresh token（高敏感，上线前须加密）"`
	ExpiresAt    int64  `json:"expires_at" doc:"三方 token 过期时间（Unix 秒）"`
}

// TableName 表名。
func (OAuthIdentity) TableName() string { return "t_user_oauth" }
