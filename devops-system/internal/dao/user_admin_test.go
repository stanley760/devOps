// ID 类 JSON 数组的存储形态契约：库里只存数字数组；字符串与数字的互转全部在代码层
// （写侧 PostIDsJSON / NormalizeDeptIDs，出参侧 types.ID 的 MarshalJSON）。
package dao_test

import (
	"reflect"
	"testing"

	"github.com/stanley760/devOps/devops-system/internal/dao"
	"github.com/stanley760/devOps/devops-system/internal/types"
)

func TestPostIDsJSON(t *testing.T) {
	cases := []struct {
		name string
		in   []types.ID
		want string
	}{
		{"数字数组形态落库", []types.ID{4, 5}, "[4,5]"},
		{"单元素", []types.ID{4}, "[4]"},
		{"空集返回空串（调用方写 NULL）", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dao.PostIDsJSON(c.in); got != c.want {
				t.Fatalf("PostIDsJSON(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestPostIDsOf(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []types.ID
	}{
		{"数字形态（唯一存储形态）", "[4,5]", []types.ID{4, 5}},
		{"字符串元素视为脏数据", `["4","5"]`, []types.ID{}},
		{"混合形态视为脏数据", `["4",5]`, []types.ID{}},
		{"空串", "", []types.ID{}},
		{"非法 JSON", "abc", []types.ID{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dao.PostIDsOf(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("PostIDsOf(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("PostIDsOf(%q) = %v, want %v", c.in, got, c.want)
				}
			}
		})
	}
}

// 写读往返：PostIDsJSON 的输出必须能被 PostIDsOf 原样读回（数字数组是存储唯一形态）。
func TestPostIDsRoundTrip(t *testing.T) {
	ids := []types.ID{1, 2, 3}
	got := dao.PostIDsOf(dao.PostIDsJSON(ids))
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("round-trip = %v, want %v", got, ids)
	}
}
