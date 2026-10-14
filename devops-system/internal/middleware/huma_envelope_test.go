package middleware

import (
	"errors"
	"fmt"
	"github.com/danielgtaylor/huma/v2"
	securitymw "github.com/stanley760/devops/devops-infra/security/middleware"
	"net/http"
	"strings"
	"testing"
)

// TestHumaEnvelopeWriters 信封回调工厂表驱动金标准：断言每个场景的完整 JSON body 与状态码。
func TestHumaEnvelopeWriters(t *testing.T) {
	cases := []struct {
		name   string
		writer func(huma.Context, error)
		err    error
		status int
		body   string
	}{
		{"401 未带token", writeHuma401, securitymw.ErrUnauthorized, http.StatusUnauthorized, `{"code":401,"msg":"未登录或会话已过期","data":null}`},
		{"401 会话失效(wrapped)", writeHuma401, fmt.Errorf("session: %w", securitymw.ErrSessionInactive), http.StatusUnauthorized, `{"code":401,"msg":"登录状态已失效，请重新登录","data":null}`},
		{"401 认证基础设施错误", writeHumaAuthInfraErr, errors.New("redis down"), http.StatusUnauthorized, `{"code":401,"msg":"登录状态已失效，请重新登录","data":null}`},
		{"403 租户不可用", writeHuma403Tenant, nil, http.StatusForbidden, `{"code":403,"msg":"租户不可用（停用或已过期）","data":null}`},
		{"403 域内鉴权拒绝", writeHuma403Perm, nil, http.StatusForbidden, `{"code":403,"msg":"无操作权限","data":null}`},
		{"403 非平台管理员(wrapped)", writeHuma403Platform, fmt.Errorf("gate: %w", securitymw.ErrNotPlatformAdmin), http.StatusForbidden, `{"code":403,"msg":"需要平台管理员权限","data":null}`},
		{"403 平台Enforce拒绝", writeHuma403Platform, securitymw.ErrForbidden, http.StatusForbidden, `{"code":403,"msg":"无操作权限","data":null}`},
		{"403 鉴权基础设施错误", writeHumaAuthzInfraErr, errors.New("casbin down"), http.StatusForbidden, `{"code":403,"msg":"无操作权限","data":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newMWAPI(t, func(hctx huma.Context, _ func(huma.Context)) { tc.writer(hctx, tc.err) })
			resp := api.Get("/")
			if resp.Code != tc.status {
				t.Fatalf("status: want %d got %d %s", tc.status, resp.Code, resp.Body.String())
			}
			if got := strings.TrimSpace(resp.Body.String()); got != tc.body {
				t.Fatalf("body: want %s got %s", tc.body, got)
			}
		})
	}
}
