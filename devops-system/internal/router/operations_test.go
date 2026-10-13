// withClientMeta 回归：geo-ip 插件注入头的 URL 反转义、脏头兜底与 XFF 真实 IP 解析。
package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humagin "github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	apiv1 "github.com/stanley760/devOps/devops-system/internal/api/v1"
	"github.com/stanley760/devOps/devops-system/internal/service"
)

// TestWithClientMeta 经真实 gin+humagin 适配层跑通 withClientMeta：
// IP 取 XFF 链（gin 默认信任全部代理，测试态直接生效）、UA 直读、
// geo 四头 URL 反转义（中文归属地主场景）、非法转义序列原值兜底。
func TestWithClientMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := humagin.New(r, huma.DefaultConfig("meta-echo-test", "v1"))
	huma.Register(api, huma.Operation{
		OperationID: "meta-echo",
		Method:      http.MethodPost,
		Path:        "/meta",
		Middlewares: huma.Middlewares{withClientMeta},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body service.LoginMeta
	}, error) {
		return &struct{ Body service.LoginMeta }{Body: apiv1.ClientMetaFrom(ctx)}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/meta", nil)
	req.Header.Set("User-Agent", "meta-echo-test/1.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	// geo-ip 插件按 URL 转义写入（中文归属地是主场景）
	req.Header.Set("X-Higress-Geo-Country", "%E4%B8%AD%E5%9B%BD")                    // 中国
	req.Header.Set("X-Higress-Geo-Province", "%E9%BB%91%E9%BE%99%E6%B1%9F%E7%9C%81") // 黑龙江省
	req.Header.Set("X-Higress-Geo-City", "Hangzhou")                                 // 纯 ASCII 不转义也合法
	req.Header.Set("X-Higress-Geo-Isp", "%zz-bad-escape")                            // 非法转义序列：原值兜底

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got service.LoginMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body 不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	if got.IP != "203.0.113.7" {
		t.Errorf("IP = %q, want 203.0.113.7（XFF 链解析）", got.IP)
	}
	if got.UserAgent != "meta-echo-test/1.0" {
		t.Errorf("UserAgent = %q", got.UserAgent)
	}
	if got.GeoCountry != "中国" {
		t.Errorf("GeoCountry = %q, want 中国（URL 反转义）", got.GeoCountry)
	}
	if got.GeoProvince != "黑龙江省" {
		t.Errorf("GeoProvince = %q, want 黑龙江省", got.GeoProvince)
	}
	if got.GeoCity != "Hangzhou" {
		t.Errorf("GeoCity = %q, want Hangzhou", got.GeoCity)
	}
	if got.GeoIsp != "%zz-bad-escape" {
		t.Errorf("GeoIsp = %q, want 原值兜底（非法转义序列）", got.GeoIsp)
	}
}
