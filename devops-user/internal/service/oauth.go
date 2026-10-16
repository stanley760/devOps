package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/stanley760/devops/devops-infra/errs"

	"github.com/stanley760/devops/devops-user/internal/auth"
	"github.com/stanley760/devops/devops-user/internal/dao"
	"github.com/stanley760/devops/devops-user/internal/model"
	cendoauth2 "github.com/stanley760/devops/devops-user/internal/oauth2"
)

// OAuth 三方登录用例。
type OAuth struct {
	authM      *auth.Manager
	providers  map[string]cendoauth2.Provider
	states     cendoauth2.StateStore
	stateTTL   time.Duration
	authzNames map[string]string // provider → 昵称前缀
	autoReg    map[string]bool   // provider → 是否自动建号
	// redirectAllow 登录后跳回地址的白名单前缀（防 open redirect）。
	redirectAllow []string
}

// OAuthDeps OAuth 用例依赖。
type OAuthDeps struct {
	AuthM     *auth.Manager
	Providers map[string]cendoauth2.Provider
	States    cendoauth2.StateStore
	StateTTL  time.Duration
	// AutoRegister 未绑定时是否自动建号（按 provider）。nil 表示全部不自动建号。
	AutoRegister map[string]bool
	// NicknamePrefix 自动建号时的昵称前缀（按 provider）。
	NicknamePrefix map[string]string
	// RedirectWhitelist 允许的跳回地址前缀（防 open redirect / 钓鱼）。
	RedirectWhitelist []string
}

// NewOAuth 构造 OAuth 用例。
func NewOAuth(d OAuthDeps) *OAuth {
	if d.StateTTL <= 0 {
		d.StateTTL = 10 * time.Minute
	}
	return &OAuth{
		authM:         d.AuthM,
		providers:     d.Providers,
		states:        d.States,
		stateTTL:      d.StateTTL,
		autoReg:       d.AutoRegister,
		authzNames:    d.NicknamePrefix,
		redirectAllow: d.RedirectWhitelist,
	}
}

// SafeRedirect 校验登录后跳回地址是否在白名单内。
//
// ⚠️ **不校验会造成 open redirect（钓鱼）**：攻击者构造
//
//	/oauth/callback?...&redirect_after=https://evil.example
//
// 受害者登录后被跳到攻击者站点，token 落到对方手里。
//
// 白名单为空时一律拒绝——保守取向：宁可不出跳回地址，
// 也不能让任意 URL 成为合法跳回目标。
func (s *OAuth) SafeRedirect(after string) (string, error) {
	if after == "" {
		return "", nil
	}
	if len(s.redirectAllow) == 0 {
		return "", errs.New(errs.CodeParam, "未配置跳回白名单，已拒绝该跳回地址")
	}
	for _, prefix := range s.redirectAllow {
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(after, prefix) {
			return after, nil
		}
	}
	return "", errs.New(errs.CodeParam, "跳回地址不在白名单内")
}

// ProviderStatus 某提供方的可用状态（供前端决定是否展示入口）。
type ProviderStatus struct {
	Name         string `json:"name" doc:"提供方标识"`
	Enabled      bool   `json:"enabled" doc:"是否可用（凭据齐备）"`
	AuthorizeURL string `json:"authorize_url,omitempty" doc:"授权跳转地址（state 已生成，一次性）"`
}

// ListProviders 返回各提供方状态，并为每个生成授权 URL（含一次性 state）。
func (s *OAuth) ListProviders(ctx context.Context) ([]ProviderStatus, error) {
	if s == nil || len(s.providers) == 0 {
		return nil, errs.New(errs.CodeInternal, "三方登录未配置")
	}
	out := make([]ProviderStatus, 0, len(s.providers))
	for name, p := range s.providers {
		st := ProviderStatus{Name: name, Enabled: p.Enabled()}
		if !p.Enabled() {
			// 凭据未配齐时也返回，前端据此隐藏入口而非报错。
			out = append(out, st)
			continue
		}
		state, err := generateState()
		if err != nil {
			return nil, errs.New(errs.CodeInternal, "生成 state 失败")
		}
		if err := s.states.Save(ctx, state, cendoauth2.StateValue{Provider: name}, s.stateTTL); err != nil {
			return nil, errs.New(errs.CodeInternal, "保存 state 失败")
		}
		st.AuthorizeURL = p.AuthCodeURL(state)
		out = append(out, st)
	}
	return out, nil
}

// StartBind 已登录用户主动绑定三方账号：生成 state 并标记 BindUserID。
func (s *OAuth) StartBind(ctx context.Context, providerName, redirectAfter string, userID int64) (string, error) {
	p, err := s.provider(providerName)
	if err != nil {
		return "", err
	}
	if !p.Enabled() {
		return "", errs.New(errs.CodeParam, "该三方登录未配置")
	}
	// 跳回地址必须先过白名单（防 open redirect）。
	safeAfter, err := s.SafeRedirect(redirectAfter)
	if err != nil {
		return "", err
	}
	state, err := generateState()
	if err != nil {
		return "", errs.New(errs.CodeInternal, "生成 state 失败")
	}
	if err := s.states.Save(ctx, state, cendoauth2.StateValue{
		Provider:      providerName,
		RedirectAfter: safeAfter,
		BindUserID:    userID,
	}, s.stateTTL); err != nil {
		return "", errs.New(errs.CodeInternal, "保存 state 失败")
	}
	return p.AuthCodeURL(state), nil
}

// OAuthResult 三方登录/绑定结果。
type OAuthResult struct {
	// TokenPair 签发的双 token（绑定场景为空）。
	TokenPair *TokenPair `json:"token_pair,omitempty" doc:"登录成功时下发；绑定场景为空"`
	// Bound 为 true 表示本次是「绑定」而非「登录」。
	Bound bool `json:"bound" doc:"本次操作是绑定（true）还是登录（false）"`
	// UserID 本地用户 ID。
	UserID int64 `json:"user_id" doc:"本地用户 ID"`
	// Nickname 本地昵称。
	Nickname string `json:"nickname" doc:"本地昵称"`
}

// Callback 处理三方回调：校验 state → 换 token → 拉信息 → 登录或绑定。
//
// 两种路径（由 state.BindUserID 决定）：
//   - BindUserID == 0：登录。已有绑定则签发双token；无绑定则按 autoReg 决定
//     自动建号还是报错要求先登录；
//   - BindUserID > 0：绑定。绑到该本地用户；若三方账号已绑给别人则拒绝。
func (s *OAuth) Callback(ctx context.Context, providerName, code, state string) (*OAuthResult, error) {
	p, err := s.provider(providerName)
	if err != nil {
		return nil, err
	}
	if code == "" {
		return nil, errs.New(errs.CodeParam, "缺少授权码 code")
	}
	// state 一次性校验：失败即 CSRF 攻击或过期重放。
	sv, err := s.states.Verify(ctx, state)
	if err != nil {
		return nil, errs.New(errs.CodeUnauthorized, "state 校验失败，请重新发起登录")
	}
	// state 里记的provider 必须与回调路径一致，防「用A 的 state 走 B 的回调」。
	if sv.Provider != providerName {
		return nil, errs.New(errs.CodeUnauthorized, "state 与提供方不匹配")
	}

	info, accessTok, refreshTok, err := p.Exchange(ctx, code)
	if err != nil {
		return nil, errs.New(errs.CodeUnauthorized, "三方授权校验失败")
	}
	if info == nil || info.ProviderUserID == "" {
		return nil, errs.New(errs.CodeUnauthorized, "三方未返回用户标识")
	}
	prov := model.OAuthProvider(providerName)

	identity, err := dao.GetOAuthIdentity(ctx, prov, info.ProviderUserID)
	if err != nil {
		return nil, err
	}

	// 路径一：已登录用户主动绑定。
	if sv.BindUserID > 0 {
		if identity != nil {
			if identity.UserID == sv.BindUserID {
				// 重复绑定同一账号：幂等，刷新 token 即可。
				_ = dao.UpdateOAuthToken(ctx, identity.ID, accessTok, refreshTok, 0, info.Nickname, info.AvatarURL)
				return &OAuthResult{Bound: true, UserID: sv.BindUserID}, nil
			}
			return nil, errs.New(errs.CodeParam, "该三方账号已绑定其他用户")
		}
		// 绑定前校验该用户确实存在且未被封禁（否则可绑到已注销账号）。
		u, err := dao.GetUserByID(ctx, sv.BindUserID)
		if err != nil {
			return nil, err
		}
		if u.Disabled {
			return nil, errs.New(errs.CodeForbidden, "账号已被封禁")
		}
		if err := dao.CreateOAuthIdentity(ctx, &model.OAuthIdentity{
			UserID:         sv.BindUserID,
			Provider:       prov,
			ProviderUserID: info.ProviderUserID,
			Nickname:       info.Nickname,
			AvatarURL:      info.AvatarURL,
			AccessToken:    accessTok,
			RefreshToken:   refreshTok,
		}); err != nil {
			return nil, err
		}
		return &OAuthResult{Bound: true, UserID: sv.BindUserID, Nickname: u.Nickname}, nil
	}

	// 路径二：登录。已有绑定直接签发。
	if identity != nil {
		u, err := dao.GetUserByID(ctx, identity.UserID)
		if err != nil {
			return nil, err
		}
		if u.Disabled {
			return nil, errs.New(errs.CodeForbidden, "账号已被封禁")
		}
		_ = dao.UpdateOAuthToken(ctx, identity.ID, accessTok, refreshTok, 0, info.Nickname, info.AvatarURL)
		pair, err := s.issue(ctx, u.ID, u.Username)
		if err != nil {
			return nil, err
		}
		return &OAuthResult{TokenPair: &pair, UserID: u.ID, Nickname: u.Nickname}, nil
	}

	// 未绑定：按配置决定是否自动建号。
	if !s.autoReg[providerName] {
		return nil, errs.New(errs.CodeParam, "该三方账号尚未绑定，请先登录后绑定")
	}
	u, err := s.autoCreateUser(ctx, providerName, info)
	if err != nil {
		return nil, err
	}
	// 建号即绑定（放在建号之后：若绑定失败会留下孤儿账号，
	// 但反过来先绑定再建号失败会留下指向不存在用户的绑定，更糟）。
	if err := dao.CreateOAuthIdentity(ctx, &model.OAuthIdentity{
		UserID:         u.ID,
		Provider:       prov,
		ProviderUserID: info.ProviderUserID,
		Nickname:       info.Nickname,
		AvatarURL:      info.AvatarURL,
		AccessToken:    accessTok,
		RefreshToken:   refreshTok,
	}); err != nil {
		return nil, err
	}
	pair, err := s.issue(ctx, u.ID, u.Username)
	if err != nil {
		return nil, err
	}
	return &OAuthResult{TokenPair: &pair, UserID: u.ID, Nickname: u.Nickname}, nil
}

// autoCreateUser 三方首次登录时自动创建本地账号。
func (s *OAuth) autoCreateUser(ctx context.Context, providerName string, info *cendoauth2.UserInfo) (*model.User, error) {
	// 用户名：优先用三方昵称（清洗为合法字符），冲突时加数字后缀。
	base := sanitizeUsername(info.Nickname)
	if base == "" {
		base = providerName + "_user"
	}
	username := base
	for i := 1; i <= 20; i++ {
		taken, err := dao.IsUsernameTaken(ctx, username)
		if err != nil {
			return nil, err
		}
		if !taken {
			break
		}
		username = fmt.Sprintf("%s%d", base, i)
	}
	// 自动建号的账号**没有可用口令**：随机 32 字节不可记忆的强随机串，
	// 杜绝「猜默认密码」或「空密码」被登录。
	random, err := generateRandomSecret()
	if err != nil {
		return nil, errs.New(errs.CodeInternal, "生成初始口令失败")
	}
	hash, err := hashPassword(random)
	if err != nil {
		return nil, errs.New(errs.CodeInternal, "口令加密失败")
	}
	nickname := info.Nickname
	if prefix := s.authzNames[providerName]; prefix != "" {
		nickname = prefix + nickname
	}
	if nickname == "" {
		nickname = username
	}
	u := &model.User{
		Username: username,
		Password: hash,
		Nickname: nickname,
	}
	if info.Email != "" {
		// ⚠️ 三方 email 可能未验证，此处仅作展示，**不用于找回密码或账号关联**。
		u.Email = &info.Email
	}
	if err := dao.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Unbind 解绑三方账号。
func (s *OAuth) Unbind(ctx context.Context, userID int64, providerName string) error {
	if !model.OAuthProvider(providerName).Valid() {
		return errs.New(errs.CodeParam, "不支持的三方提供方")
	}
	return dao.DeleteOAuthIdentity(ctx, userID, model.OAuthProvider(providerName))
}

// ListBound 列已绑定的三方账号。
func (s *OAuth) ListBound(ctx context.Context, userID int64) ([]model.OAuthIdentity, error) {
	list, err := dao.ListOAuthIdentities(ctx, userID)
	if err != nil {
		return nil, err
	}
	// 不返回 token 字段（虽已标 json:"-"，这里再兜一层避免误用）。
	for i := range list {
		list[i].AccessToken = ""
		list[i].RefreshToken = ""
	}
	return list, nil
}

func (s *OAuth) issue(ctx context.Context, userID int64, username string) (TokenPair, error) {
	pair, err := s.authM.Issue(ctx, userID, username)
	if err != nil {
		return TokenPair{}, errs.New(errs.CodeInternal, "签发令牌失败")
	}
	return pair, nil
}

func (s *OAuth) provider(name string) (cendoauth2.Provider, error) {
	p, ok := s.providers[name]
	if !ok {
		return nil, errs.New(errs.CodeParam, "不支持的三方登录提供方")
	}
	return p, nil
}

// generateState 生成 256 位随机 state（防 CSRF 的关键，须不可预测）。
func generateState() (string, error) {
	return generateRandomSecret()
}

// generateRandomSecret 生成 32 字节随机串的 base64url 形式。
func generateRandomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// sanitizeUsername 把三方昵称清洗为合法的本地用户名。
// 非法字符替换为下划线；清洗后为空则返回空串由调用方决定。
func sanitizeUsername(s string) string {
	if s == "" {
		return ""
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
		if len(out) >= 24 { // 控制长度，留后缀空间
			break
		}
	}
	res := string(out)
	for len(res) > 0 && res[0] == '_' {
		// 必须以字母开头（usernamePattern 要求）
		res = res[1:]
	}
	return res
}

