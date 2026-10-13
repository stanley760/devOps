package nacos

import (
	"net/url"
	"testing"

	"google.golang.org/grpc/resolver"
)

// TestServiceNameFromTarget 验证 target 两种写法都能提取服务名。
// 历史缺陷：nacos://devops-id（两斜杠）会把服务名解析为 authority，
// grpc resolver.Target.Endpoint() 返回空 → Build 报错 → 连接永远无地址。
func TestServiceNameFromTarget(t *testing.T) {
	cases := []struct {
		target string
		want   string
	}{
		{"nacos:///devops-id", "devops-id"},            // 标准三斜杠：服务名在 path 位
		{"nacos://devops-id", "devops-id"},             // 两斜杠误写：服务名在 authority 位，Host 兜底
		{"nacos:///devops-id?timeout=5s", "devops-id"}, // 带查询参数
		{"nacos:///", ""},                              // 无服务名 → Build 应报错
	}

	for _, c := range cases {
		u, err := url.Parse(c.target)
		if err != nil {
			t.Fatalf("parse %q: %v", c.target, err)
		}
		got := serviceNameFromTarget(resolver.Target{URL: *u})
		if got != c.want {
			t.Errorf("serviceNameFromTarget(%q) = %q, want %q", c.target, got, c.want)
		}
	}
}
