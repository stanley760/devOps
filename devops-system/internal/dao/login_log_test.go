// 登录审计 dao 纯函数测试：clipRunes 按（多字节）字符截断，杜绝非法 UTF-8 入库。
package dao

import (
	"strings"
	"testing"
)

func TestClipRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"短串原样", "admin", 30, "admin"},
		{"字节数不超即原样（快路径）", "中国", 64, "中国"},
		{"中文超长按字符截断", "黑龙江省哈尔滨市道里区", 3, "黑龙江"},
		{"ASCII 截断", strings.Repeat("a", 100), 64, strings.Repeat("a", 64)},
		{"中英混合按字符计", "中国abc中国abc", 5, "中国abc"},
		{"空串", "", 30, ""},
		{"零上限", "abc", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clipRunes(c.in, c.n); got != c.want {
				t.Fatalf("clipRunes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
			}
		})
	}
}
