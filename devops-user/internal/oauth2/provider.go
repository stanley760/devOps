package oauth2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Provider 是三方登录提供方的抽象。
type Provider interface {
	// Name 提供方标识。
	Name() string
	// AuthCodeURL 生成授权跳转地址（含 state）。
	AuthCodeURL(state string) string
	// Exchange 用授权码换 token，再拉取用户信息。
	//
	// ctx 应带 oauth2.HTTPClient（本包已设置），否则走默认客户端。
	Exchange(ctx context.Context, code string) (*UserInfo, string, string, error)
	// Enabled 该提供方是否可用（凭据齐备）。
	Enabled() bool
}

// httpClient 三方交互用的 HTTP 客户端。
//
// 显式设超时：http.DefaultClient 无超时，对端卡住会把请求挂死，
// 登录链路上的挂起会累积成雪崩。
var httpClient = &http.Client{Timeout: 10 * time.Second}

// oauthCtx 附加 oauth2 库所需的 HTTP 客户端。
//
// 不能设 oauth2.HTTPClient 包级变量（那是 context key 类型，非客户端），
// 只能在调Exchange 时用 context 传入。
func oauthCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, httpClient)
}

// ---------------------------------------------------------------- GitHub

// GitHubProvider GitHub OAuth（不实现 OIDC，无 id_token）。
type GitHubProvider struct {
	cfg    oauth2.Config
	apiURL string // 便于测试注入 mock
}

// NewGitHubProvider 构造 GitHub provider。
func NewGitHubProvider(cfg Config) *GitHubProvider {
	return &GitHubProvider{
		cfg: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
		},
		apiURL: "https://api.github.com",
	}
}

// Name 实现 Provider。
func (p *GitHubProvider) Name() string { return "github" }

// Enabled 实现 Provider。
func (p *GitHubProvider) Enabled() bool {
	return p.cfg.ClientID != "" && p.cfg.ClientSecret != "" && p.cfg.RedirectURL != ""
}

// AuthCodeURL 实现 Provider。
func (p *GitHubProvider) AuthCodeURL(state string) string { return p.cfg.AuthCodeURL(state) }

// githubUser 是 GitHub /user 响应。
type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// Exchange 实现 Provider：换 token 后调 /user 拉信息。
func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*UserInfo, string, string, error) {
	tok, err := p.cfg.Exchange(oauthCtx(ctx), code)
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: github token 交换失败", ErrExchangeFailed)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiURL+"/user", nil)
	if err != nil {
		return nil, "", "", err
	}
	// GitHub 的 API 是 Bearer 认证。
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devops-user")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("%w: github /user 返回 %d", ErrExchangeFailed, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", "", err
	}
	var gu githubUser
	if err := json.Unmarshal(body, &gu); err != nil {
		return nil, "", "", fmt.Errorf("解析 github 用户信息失败: %w", err)
	}
	if gu.ID == 0 {
		return nil, "", "", fmt.Errorf("%w: github 未返回用户 id", ErrExchangeFailed)
	}
	nickname := gu.Name
	if nickname == "" {
		nickname = gu.Login
	}
	return &UserInfo{
		// GitHub 的数字 ID 是稳定标识；**不可用 login**（可改名）。
		ProviderUserID: fmt.Sprintf("%d", gu.ID),
		Nickname:       nickname,
		Email:          gu.Email, // 可能为公开邮箱，未经验证
		AvatarURL:      gu.AvatarURL,
	}, tok.AccessToken, tok.RefreshToken, nil
}

// ---------------------------------------------------------------- Google

// GoogleProvider Google OAuth 2.0（OIDC）。
type GoogleProvider struct {
	cfg       oauth2.Config
	verifyURL string // 便于测试注入 mock
}

// NewGoogleProvider 构造 Google provider。
func NewGoogleProvider(cfg Config) *GoogleProvider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{
			"openid",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		}
	}
	return &GoogleProvider{
		cfg: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
		},
		verifyURL: "https://oauth2.googleapis.com/tokeninfo",
	}
}

// Name 实现 Provider。
func (p *GoogleProvider) Name() string { return "google" }

// Enabled 实现 Provider。
func (p *GoogleProvider) Enabled() bool {
	return p.cfg.ClientID != "" && p.cfg.ClientSecret != "" && p.cfg.RedirectURL != ""
}

// AuthCodeURL 实现 Provider。
func (p *GoogleProvider) AuthCodeURL(state string) string { return p.cfg.AuthCodeURL(state) }

// Exchange 实现 Provider。
//
// ⚠️ **id_token 必须验签**：Google 的 id_token 是 JWT，但 decode 后不能直接信。
// 这里调tokeninfo 端点校验（等价验签 + 校验 iss/aud/exp），比本地验签更简单
// 且不易漏校验项（nonce、azp 等）。
func (p *GoogleProvider) Exchange(ctx context.Context, code string) (*UserInfo, string, string, error) {
	tok, err := p.cfg.Exchange(oauthCtx(ctx), code)
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: google token 交换失败", ErrExchangeFailed)
	}
	if tok.AccessToken == "" || !strings.HasPrefix(tok.TokenType, "Bearer") && tok.TokenType == "" {
		return nil, "", "", fmt.Errorf("%w: google 未返回 access token", ErrExchangeFailed)
	}
	info, err := p.verifyIDToken(ctx, tok.AccessToken)
	if err != nil {
		return nil, "", "", err
	}
	return info, tok.AccessToken, tok.RefreshToken, nil
}

// googleTokenInfo 是 tokeninfo 响应。
type googleTokenInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // 可能是 bool 或 string
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Aud           string `json:"aud"`
	Iss           string `json:"iss"`
}

// verifyIDToken 校验并解析 id_token（经 tokeninfo 端点）。
func (p *GoogleProvider) verifyIDToken(ctx context.Context, accessToken string) (*UserInfo, error) {
	url := p.verifyURL + "?access_token=" + accessToken
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: google tokeninfo 返回 %d", ErrExchangeFailed, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var ti googleTokenInfo
	if err := json.Unmarshal(body, &ti); err != nil {
		return nil, fmt.Errorf("解析 google tokeninfo 失败: %w", err)
	}
	if ti.Sub == "" {
		return nil, fmt.Errorf("%w: google id_token 缺少 sub", ErrExchangeFailed)
	}
	// aud 必须是本应用的 client_id，否则是「拿别处的 token 来冒用」。
	if p.cfg.ClientID != "" && ti.Aud != "" && ti.Aud != p.cfg.ClientID {
		return nil, fmt.Errorf("%w: google aud 不匹配", ErrExchangeFailed)
	}
	// iss 必须是 Google。
	if ti.Iss != "" && !strings.HasPrefix(ti.Iss, "https://accounts.google.com") {
		return nil, fmt.Errorf("%w: google iss 非法", ErrExchangeFailed)
	}
	return &UserInfo{
		ProviderUserID: ti.Sub, // sub 是 OIDC 稳定标识
		Nickname:       ti.Name,
		Email:          ti.Email,
		AvatarURL:      ti.Picture,
	}, nil
}

// ---------------------------------------------------------------- 微信

// WeChatProvider 微信开放平台「网站应用」扫码登录。
//
// 文档：https://developers.weixin.qq.com/doc/oplatform/Website_App/WeChat_Login/Wechat_Login.html
//
//## 与 GitHub / Google 的四点关键差异（照抄标准库必踩）
//
//  1. **授权端点不是标准 OAuth2 端点**：是 `open.weixin.qq.com/connect/qrconnect`，
//     且 URL 末尾必须带 `#wechat_redirect` fragment——**没有它微信直接报「链接无法访问」**。
//     `x/oauth2` 的 `Config.AuthCodeURL()` 生成不出这个 fragment，故本provider 手写 URL。
//  2. **`redirect_uri` 必须 urlEncode**：微信要求对整个回调地址编码。
//     而 `url.Values.Encode()` 会把 `%` 编码成 `%25`，导致双重编码。必须手工拼接。
//  3. **scope 固定 `snsapi_login`**：其他值（如 `snsapi_base`）会直接拒绝，
//     且 scope 在开放平台后台申请后不可改。
//  4. **无 id_token、无签名可验**：身份凭据是 `access_token + openid`。
//     安全性来自「code 一次性 + 10 分钟过期」+ 全程 HTTPS，
//     故必须**校验返回的 openid 非空**，不能因缺字段而放行。
//
// ## 绑定键的选择：unionid 优先，openid 兜底
//
// openid 对「当前开放平台账号」唯一，unionid 在**同一开放平台账号下的所有应用**
// （网站应用/公众号/移动应用）唯一。用 unionid 作绑定键可跨应用识别同一用户；
// 未绑定开放平台账号时 unionid 可能为空，此时退回 openid。
type WeChatProvider struct {
	clientID     string
	clientSecret string
	redirectURL  string
	// apiBase 便于测试注入 mock。
	apiBase string
}

// NewWeChatProvider 构造微信 provider。
func NewWeChatProvider(cfg Config) *WeChatProvider {
	// 微信网站应用的 scope 只有一个合法值，不接受自定义。
	_ = cfg.Scopes
	return &WeChatProvider{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		redirectURL:  cfg.RedirectURL,
		apiBase:      "https://api.weixin.qq.com",
	}
}

// Name 实现 Provider。
func (p *WeChatProvider) Name() string { return "wechat" }

// Enabled 实现 Provider。
func (p *WeChatProvider) Enabled() bool {
	return p.clientID != "" && p.clientSecret != "" && p.redirectURL != ""
}

// AuthCodeURL 实现 Provider。
//
// 手工拼 URL 而不用 url.Values.Encode()：微信要求 redirect_uri 只编码一层，
// 而 Encode() 会把已编码的 % 再编码成 %25（双重编码，导致微信解析出错误的回调地址）。
func (p *WeChatProvider) AuthCodeURL(state string) string {
	// ⚠️ redirect_uri 必须 urlEncode（含协议、端口、路径）。
	return "https://open.weixin.qq.com/connect/qrconnect" +
		"?appid=" + url.QueryEscape(p.clientID) +
		"&redirect_uri=" + url.QueryEscape(p.redirectURL) +
		"&response_type=code" +
		// 网页应用目前仅支持 snsapi_login，填其他值会被微信拒。
		"&scope=snsapi_login" +
		"&state=" + url.QueryEscape(state) +
		// ⚠️ 必须以 #wechat_redirect 结尾，否则微信提示「该链接无法访问」。
		"#wechat_redirect"
}

// wechatTokenResp 是 /sns/oauth2/access_token 的响应。
//
// ⚠️ 微信所有接口**出错时也返回 HTTP 200**，错误在 body 的 errcode 里。
// 只判 HTTP 状态码会把失败当成功，必须显式判errcode。
type wechatTokenResp struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid"`
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
}

// wechatUserInfo 是 /sns/userinfo 的响应。
//
// ⚠️ 2021-10-20 起微信不再返回 sex / province / city / country，故此处不含这些字段。
type wechatUserInfo struct {
	OpenID     string   `json:"openid"`
	Nickname   string   `json:"nickname"`
	HeadImgURL string   `json:"headimgurl"`
	UnionID    string   `json:"unionid"`
	Privilege  []string `json:"privilege"`
	ErrCode    int      `json:"errcode"`
	ErrMsg     string   `json:"errmsg"`
}

// Exchange 实现 Provider：换 access_token → 拉用户信息。
func (p *WeChatProvider) Exchange(ctx context.Context, code string) (*UserInfo, string, string, error) {
	tr, err := p.fetchToken(ctx, code)
	if err != nil {
		return nil, "", "", err
	}
	info, err := p.fetchUserInfo(ctx, tr.AccessToken, tr.OpenID)
	if err != nil {
		return nil, "", "", err
	}
	// 绑定键：unionid 优先（跨应用稳定），缺失时退回 openid。
	//
	// ⚠️ 两者都空说明微信返回异常响应，必须拒绝——否则会写入空绑定键，
	// 使所有「微信未返回标识」的账号互相错绑。
	providerUserID := info.UnionID
	if providerUserID == "" {
		providerUserID = tr.UnionID
	}
	if providerUserID == "" {
		providerUserID = tr.OpenID
	}
	if providerUserID == "" {
		return nil, "", "", fmt.Errorf("%w: 微信未返回 openid/unionid", ErrExchangeFailed)
	}
	if info.OpenID == "" {
		info.OpenID = tr.OpenID
	}
	return &UserInfo{
		ProviderUserID: providerUserID,
		Nickname:       info.Nickname,
		//⚠️ 微信接口**不返回邮箱**（且不允许索取），故 Email 恒为空。
		// 这意味着「邮箱找回密码」对纯微信用户不可用，需另设绑定手机号/邮箱流程。
		AvatarURL: p.normalizeAvatar(info.HeadImgURL),
	}, tr.AccessToken, tr.RefreshToken, nil
}

// fetchToken 用 code 换 access_token。
//
// 接口：GET /sns/oauth2/access_token
//	?appid=&secret=&code=&grant_type=authorization_code
func (p *WeChatProvider) fetchToken(ctx context.Context, code string) (*wechatTokenResp, error) {
	q := url.Values{}
	q.Set("appid", p.clientID)
	q.Set("secret", p.clientSecret)
	q.Set("code", code)
	q.Set("grant_type", "authorization_code")
	body, err := p.getJSON(ctx, p.apiBase+"/sns/oauth2/access_token?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("%w: 微信 token 交换请求失败: %v", ErrExchangeFailed, err)
	}
	var tr wechatTokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("%w: 解析微信 token 响应失败: %v", ErrExchangeFailed, err)
	}
	// 微信错误也返回 200，必须显式判 errcode。常见：
	//   40029 invalid code（code 过期/已用/不属于本appid）
	//   40013 invalid appid   40125 invalid appsecret
	if tr.ErrCode != 0 {
		return nil, fmt.Errorf("%w: 微信返回 errcode=%d errmsg=%s", ErrExchangeFailed, tr.ErrCode, tr.ErrMsg)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("%w: 微信未返回 access_token", ErrExchangeFailed)
	}
	return &tr, nil
}

// fetchUserInfo 拉取用户基本信息。
//
// 接口：GET /sns/userinfo?access_token=&openid=
func (p *WeChatProvider) fetchUserInfo(ctx context.Context, accessToken, openID string) (*wechatUserInfo, error) {
	q := url.Values{}
	q.Set("access_token", accessToken)
	q.Set("openid", openID)
	q.Set("lang", "zh_CN")
	body, err := p.getJSON(ctx, p.apiBase+"/sns/userinfo?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("%w: 微信用户信息请求失败: %v", ErrExchangeFailed, err)
	}
	var ui wechatUserInfo
	if err := json.Unmarshal(body, &ui); err != nil {
		return nil, fmt.Errorf("%w: 解析微信用户信息失败: %v", ErrExchangeFailed, err)
	}
	// 40003 invalid openid / 42001 access_token 超时
	if ui.ErrCode != 0 {
		return nil, fmt.Errorf("%w: 微信用户信息 errcode=%d errmsg=%s", ErrExchangeFailed, ui.ErrCode, ui.ErrMsg)
	}
	return &ui, nil
}

// getJSON 发起 GET 并读取响应体。
func (p *WeChatProvider) getJSON(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// normalizeAvatar 规整头像 URL。
//
// 微信头像 URL 末尾的 `/0` 表示 640×640，`/46`~`/132` 是小图。
// 统一取 `/0` 拿原图，且去掉协议前缀带来的 http/https 混用问题。
//
// ⚠️ **微信头像 URL 在用户改头像后即失效**。故它只能作为「展示快照」，
// 重要场景须把图片转存到自己存储（本项目当前未做，见 model/oauth.go 的 AvatarURL 注释）。
func (p *WeChatProvider) normalizeAvatar(raw string) string {
	if raw == "" {
		return ""
	}
	// 去掉末尾的尺寸段（形如 .../0 或 .../96），再补 /0 取原图。
	if i := strings.LastIndex(raw, "/"); i > 0 {
		last := raw[i+1:]
		switch last {
		case "0", "46", "64", "96", "132":
			raw = raw[:i]
		}
	}
	return raw + "/0"
}

// errNotImplemented 未实现占位错误。
type errNotImplemented string

func (e errNotImplemented) Error() string { return string(e) }
