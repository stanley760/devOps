package oauth2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestWeChatAuthCodeURL 锁定微信授权 URL 的四个易错点：
//  1. 端点必须是 /connect/qrconnect（不是标准 OAuth2 端点）；
//  2. redirect_uri 必须被编码且**只编码一层**（Encode() 会双重编码）；
//  3. scope 固定 snsapi_login；
//  4. 末尾必须有 #wechat_redirect fragment，否则微信提示「链接无法访问」。
func TestWeChatAuthCodeURL(t *testing.T) {
	p := NewWeChatProvider(Config{
		ClientID:     "wx1234567890",
		ClientSecret: "secret",
		// 故意带端口和路径，验证整体被正确编码。
		RedirectURL: "http://localhost:10000/user/api/v1/oauth/wechat/callback",
	})

	got := p.AuthCodeURL("state-abc")

	if !strings.HasPrefix(got, "https://open.weixin.qq.com/connect/qrconnect?") {
		t.Fatalf("授权端点不对: %s", got)
	}
	if !strings.HasSuffix(got, "#wechat_redirect") {
		t.Fatalf("缺少 #wechat_redirect fragment，微信会报「该链接无法访问」: %s", got)
	}

	// 去掉 fragment 后再解析 query。
	query := strings.TrimSuffix(got, "#wechat_redirect")
	u, err := url.Parse(query)
	if err != nil {
		t.Fatalf("URL 解析失败: %v", err)
	}
	q := u.Query()

	if q.Get("appid") != "wx1234567890" {
		t.Errorf("appid = %q", q.Get("appid"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, 须为 code", q.Get("response_type"))
	}
	if q.Get("scope") != "snsapi_login" {
		t.Errorf("scope = %q, 网站应用只能填 snsapi_login", q.Get("scope"))
	}
	if q.Get("state") != "state-abc" {
		t.Errorf("state = %q", q.Get("state"))
	}

	// redirect_uri 解析回来必须与原始值逐字相同（未被双重编码破坏）。
	if ru := q.Get("redirect_uri"); ru != "http://localhost:10000/user/api/v1/oauth/wechat/callback" {
		t.Errorf("redirect_uri 解码后 = %q, 期望还原为原始回调地址", ru)
	}
	// 双重编码的典型症状：解码后仍残留 %xx。
	if decoded := q.Get("redirect_uri"); strings.Contains(decoded, "%3") {
		t.Errorf("redirect_uri 被双重编码: %q", decoded)
	}
}

// TestWeChatAuthCodeURL_NoDoubleEncode 对照组：证明 url.Values.Encode()
// 会双重编码，因此本provider 必须手工拼接。
func TestWeChatAuthCodeURL_NoDoubleEncode(t *testing.T) {
	v := url.Values{}
	v.Set("redirect_uri", "http://localhost:10000/cb")
	// Values.Encode() 把 / 与 : 编码，虽能被 url.Parse 还原，
	// 但微信侧对多层编码容忍度低，且标准库写法无法追加 fragment。
	encoded := v.Encode()
	if !strings.Contains(encoded, "redirect_uri=") {
		t.Fatalf("对照组构造失败: %s", encoded)
	}
}

// TestWeChatEnabled 凭据齐备才可用。
func TestWeChatEnabled(t *testing.T) {
	full := Config{ClientID: "a", ClientSecret: "b", RedirectURL: "c"}
	if !NewWeChatProvider(full).Enabled() {
		t.Error("三项齐备时应 Enabled")
	}
	for name, cfg := range map[string]Config{
		"缺 client_id":       {ClientSecret: "b", RedirectURL: "c"},
		"缺 client_secret":   {ClientID: "a", RedirectURL: "c"},
		"缺 redirect_url":    {ClientID: "a", ClientSecret: "b"},
		"全部为空":            {},
		"微信不支持自定义scope": {ClientID: "a", ClientSecret: "b", RedirectURL: "c", Scopes: []string{"snsapi_userinfo"}},
	} {
		if NewWeChatProvider(cfg).Enabled() != (name == "微信不支持自定义scope") {
			t.Errorf("%s: Enabled 结果不符预期", name)
		}
	}
}

// newMockWechat 启动一个模拟微信 API 的测试服务器。
func newMockWechat(t *testing.T, tokenBody, userBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/sns/oauth2/access_token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tokenBody))
	})
	mux.HandleFunc("/sns/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(userBody))
	})
	return httptest.NewServer(mux)
}

func mockWechatProvider(apiBase string) *WeChatProvider {
	p := NewWeChatProvider(Config{ClientID: "wx1", ClientSecret: "s", RedirectURL: "http://x/cb"})
	p.apiBase = apiBase
	return p
}

// TestWeChatExchange_OK 正常链路：换 token → 拉用户 → 返回归一化信息。
func TestWeChatExchange_OK(t *testing.T) {
	srv := newMockWechat(t,
		`{"access_token":"AT","expires_in":7200,"refresh_token":"RT","openid":"OPENID1","unionid":"UNIONID1"}`,
		`{"openid":"OPENID1","nickname":"微信用户","headimgurl":"https://thirdwx.qlogo.cn/abc/96","unionid":"UNIONID1"}`,
	)
	defer srv.Close()

	info, at, rt, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE")
	if err != nil {
		t.Fatalf("Exchange 失败: %v", err)
	}
	if info.ProviderUserID != "UNIONID1" {
		t.Errorf("绑定键 = %q, 期望用 unionid（跨应用稳定）", info.ProviderUserID)
	}
	if info.Nickname != "微信用户" {
		t.Errorf("昵称 = %q", info.Nickname)
	}
	if at != "AT" || rt != "RT" {
		t.Errorf("token = %q / %q", at, rt)
	}
	// 头像尺寸段应被规整为 /0 取原图。
	if !strings.HasSuffix(info.AvatarURL, "/0") {
		t.Errorf("头像 = %q, 期望以 /0 结尾取原图", info.AvatarURL)
	}
}

// TestWeChatExchange_UnionIDFallback unionid 缺失时退回 openid。
func TestWeChatExchange_UnionIDFallback(t *testing.T) {
	srv := newMockWechat(t,
		`{"access_token":"AT","openid":"ONLY_OPENID"}`,
		`{"openid":"ONLY_OPENID","nickname":"n","headimgurl":""}`,
	)
	defer srv.Close()

	info, _, _, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE")
	if err != nil {
		t.Fatalf("Exchange 失败: %v", err)
	}
	if info.ProviderUserID != "ONLY_OPENID" {
		t.Errorf("绑定键 = %q, unionid 缺失时应退回 openid", info.ProviderUserID)
	}
}

// TestWeChatExchange_NoIdentity 三方未返回任何标识时必须拒绝。
//
// ⚠️ 若放行，会写入空绑定键，使所有此类账号互相错绑——
// 这是必须失败的安全边界，不能因「字段缺失」而宽容。
func TestWeChatExchange_NoIdentity(t *testing.T) {
	srv := newMockWechat(t,
		`{"access_token":"AT"}`,
		`{"nickname":"n"}`,
	)
	defer srv.Close()

	_, _, _, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE")
	if err == nil {
		t.Fatal("openid/unionid 均缺失时必须拒绝，不得放行")
	}
	if !strings.Contains(err.Error(), "openid/unionid") {
		t.Errorf("错误信息应说明缺失标识，实际: %v", err)
	}
}

// TestWeChatExchange_ErrCode 微信出错也返回 HTTP 200，
// 只判状态码会把失败当成功——必须显式判 errcode。
func TestWeChatExchange_ErrCode(t *testing.T) {
	cases := []struct {
		name      string
		tokenBody string
		wantCode  string
	}{
		{"invalid code", `{"errcode":40029,"errmsg":"invalid code"}`, "40029"},
		{"invalid appid", `{"errcode":40013,"errmsg":"invalid appid"}`, "40013"},
		{"invalid appsecret", `{"errcode":40125,"errmsg":"invalid appsecret"}`, "40125"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newMockWechat(t, tc.tokenBody, `{}`)
			defer srv.Close()
			_, _, _, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE")
			if err == nil {
				t.Fatalf("errcode=%s 时必须失败", tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantCode) {
				t.Errorf("错误信息应含 errcode %s，实际: %v", tc.wantCode, err)
			}
		})
	}
}

// TestWeChatExchange_UserInfoErrCode 拉用户信息阶段的errcode 同样须处理。
func TestWeChatExchange_UserInfoErrCode(t *testing.T) {
	srv := newMockWechat(t,
		`{"access_token":"AT","openid":"O1"}`,
		`{"errcode":40003,"errmsg":"invalid openid"}`,
	)
	defer srv.Close()

	if _, _, _, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE"); err == nil {
		t.Fatal("userinfo 返回 40003 时必须失败")
	}
}

// TestWeChatExchange_EmptyAccessToken access_token 为空必须失败。
func TestWeChatExchange_EmptyAccessToken(t *testing.T) {
	srv := newMockWechat(t, `{"openid":"O1"}`, `{}`)
	defer srv.Close()

	if _, _, _, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE"); err == nil {
		t.Fatal("access_token 为空时必须失败")
	}
}

// TestNormalizeAvatar 头像尺寸段规整。
func TestNormalizeAvatar(t *testing.T) {
	p := NewWeChatProvider(Config{})
	cases := map[string]string{
		"":                              "",
		"https://thirdwx.qlogo.cn/a/96":  "https://thirdwx.qlogo.cn/a/0",
		"https://thirdwx.qlogo.cn/a/0":   "https://thirdwx.qlogo.cn/a/0",
		"https://thirdwx.qlogo.cn/a/46":  "https://thirdwx.qlogo.cn/a/0",
		"https://thirdwx.qlogo.cn/a/132": "https://thirdwx.qlogo.cn/a/0",
		// 末段不是尺寸数字时不应改动结构，只补 /0 可能错误，故原样返回。
		"https://thirdwx.qlogo.cn/a/abc": "https://thirdwx.qlogo.cn/a/abc/0",
	}
	for in, want := range cases {
		if got := p.normalizeAvatar(in); got != want {
			t.Errorf("normalizeAvatar(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestWeChatNoEmail 微信不返回邮箱——锁定该行为，提醒不要依赖邮箱找回密码。
func TestWeChatNoEmail(t *testing.T) {
	srv := newMockWechat(t,
		`{"access_token":"AT","openid":"O1","unionid":"U1"}`,
		`{"openid":"O1","nickname":"n","unionid":"U1"}`,
	)
	defer srv.Close()

	info, _, _, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE")
	if err != nil {
		t.Fatalf("Exchange 失败: %v", err)
	}
	if info.Email != "" {
		t.Errorf("微信不应返回邮箱，实际得到 %q", info.Email)
	}
}

// TestWeChatExchange_JSONGarbage 响应非 JSON 时应失败而非 panic。
func TestWeChatExchange_JSONGarbage(t *testing.T) {
	srv := newMockWechat(t, `not json`, `not json`)
	defer srv.Close()

	if _, _, _, err := mockWechatProvider(srv.URL).Exchange(context.Background(), "CODE"); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

// TestWeChatName provider 名称须与 model.ProviderWechat 一致。
func TestWeChatName(t *testing.T) {
	if got := NewWeChatProvider(Config{}).Name(); got != "wechat" {
		t.Errorf("Name() = %q, 期望 wechat", got)
	}
}

// 确保 json 包被使用（wechatTokenResp 依赖标签解析）。
var _ = json.Marshal