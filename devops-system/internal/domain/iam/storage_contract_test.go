package iam

import (
	"strconv"
	"testing"
)

// TestEncodeIDArray 存储形态必须是数字数组（存量数据与唯一索引依赖此形态）。
//
// 迁移说明：这三条用例对应改造前 dao/user_admin_test.go 的
// TestPostIDsJSON / TestPostIDsOf / TestPostIDsRoundTrip，
// 是「API 的 ID 字符串化契约不得泄漏进存储层」的守门人。
func TestEncodeIDArray(t *testing.T) {
	cases := []struct {
		name string
		in   []int64
		want string
	}{
		{name: "空集合", in: nil, want: ""},
		{name: "空切片", in: []int64{}, want: ""},
		{name: "单元素", in: []int64{4}, want: "[4]"},
		{name: "多元素", in: []int64{4, 5}, want: "[4,5]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EncodeIDArray(c.in); got != c.want {
				t.Fatalf("EncodeIDArray(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestDecodeIDArray(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []int64
	}{
		{name: "空串", in: "", want: []int64{}},
		{name: "数字数组", in: "[4,5]", want: []int64{4, 5}},
		{name: "单元素", in: "[4]", want: []int64{4}},
		// 字符串元素是 API 契约形态（types.ID.MarshalJSON 的产物），
		// 在存储层视为脏数据：整体解析失败返回空集，不做兼容读。
		{name: "字符串元素视为脏数据", in: `["4","5"]`, want: []int64{}},
		{name: "非法 JSON", in: "not-json", want: []int64{}},
		{name: "null 字面量", in: "null", want: []int64{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecodeIDArray(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("DecodeIDArray(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("DecodeIDArray(%q) = %v, want %v", c.in, got, c.want)
				}
			}
		})
	}
}

func TestIDArrayRoundTrip(t *testing.T) {
	orig := []int64{4, 5, 100}
	encoded := EncodeIDArray(orig)
	if encoded != "[4,5,100]" {
		t.Fatalf("编码 = %q, want \"[4,5,100]\"（必须是数字数组）", encoded)
	}
	back := DecodeIDArray(encoded)
	if len(back) != len(orig) {
		t.Fatalf("往返长度 = %d, want %d", len(back), len(orig))
	}
	for i := range orig {
		if back[i] != orig[i] {
			t.Fatalf("往返值 = %v, want %v", back, orig)
		}
	}
}

func TestNormalizeIDArrayJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "已是数字数组", in: "[4,5]", want: "[4,5]"},
		{name: "脏数据归一为空", in: `["4"]`, want: ""},
		{name: "空串", in: "", want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeIDArrayJSON(c.in); got != c.want {
				t.Fatalf("Normalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSoftDeletedNaming 软删改写唯一键的命名格式是存储契约
// （存量软删数据与唯一索引共存依赖此格式，勿改）。
func TestSoftDeletedNaming(t *testing.T) {
	at := fixedTime()
	// 时间戳后缀按 Unix 秒拼接，期望值由同一时刻推导而非手写常量，
	// 避免测试与实现各自漂移。
	want := "alice_del_42_" + strconv.FormatInt(at.Unix(), 10)
	if got := RenameSoftDeleted("alice", 42, at); got != want {
		t.Fatalf("RenameSoftDeleted = %q, want %q", got, want)
	}
	if got, want := SoftDeletedCode("editor", 9), "editor_del_9"; got != want {
		t.Fatalf("SoftDeletedCode = %q, want %q", got, want)
	}
}

func TestIDsFromStrings(t *testing.T) {
	got := IDsFromStrings([]string{"4", " 5 ", "bad", "6"})
	want := []int64{4, 5, 6}
	if len(got) != len(want) {
		t.Fatalf("IDsFromStrings = %v, want %v（非法项应跳过）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IDsFromStrings = %v, want %v", got, want)
		}
	}
}
