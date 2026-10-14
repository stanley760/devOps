package audit

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipTruncatesByRuneNotByte(t *testing.T) {
	// 「黑龙江省」3 个字符 = 9 字节；若按字节截断会切出非法 UTF-8。
	input := "黑龙江省哈尔滨市"
	got := Clip(input, 3)

	if utf8.RuneCountInString(got) != 3 {
		t.Fatalf("Clip() runes = %d, want 3 (%q)", utf8.RuneCountInString(got), got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("Clip() produced invalid UTF-8: %q", got)
	}
	if got != "黑龙江" {
		t.Fatalf("Clip() = %q, want %q", got, "黑龙江")
	}
}

func TestClipKeepsStringWhenAlreadyWithinLimit(t *testing.T) {
	// 4 个字符 = 12 字节 > 4，但字符数 4 <= 4：按字符计数时无需截断。
	// 这正是"按字节截会误伤中文"的反面场景。
	input := "北京上海"
	got := Clip(input, 4)
	if got != input {
		t.Fatalf("Clip() = %q, want unchanged %q", got, input)
	}
}

func TestClipHandlesEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"empty", "", 5, ""},
		{"n zero", "abc", 0, ""},
		{"shorter than n", "ab", 10, "ab"},
		{"exactly n", "abcd", 4, "abcd"},
		{"ascii truncate", "abcdef", 3, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clip(tt.in, tt.n); got != tt.want {
				t.Fatalf("Clip(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

func TestClipNeverProducesInvalidUTF8(t *testing.T) {
	// 混合 ASCII 与多字节字符，逐个收紧截断点。
	input := "user-北京市-上海-abc"
	for n := 0; n <= utf8.RuneCountInString(input)+2; n++ {
		got := Clip(input, n)
		if !utf8.ValidString(got) {
			t.Fatalf("Clip(n=%d) produced invalid UTF-8: %q", n, got)
		}
		if r := utf8.RuneCountInString(got); r > n {
			t.Fatalf("Clip(n=%d) returned %d runes, exceeds limit", n, r)
		}
	}
}

func TestRecordNormalizeClipsAllBoundedFields(t *testing.T) {
	long := strings.Repeat("A", 600)
	rec := Record{
		TraceID:       long,
		Username:      long,
		UserIP:        long,
		UserAgent:     long,
		Module:        long,
		SubModule:     long,
		RequestMethod: long,
		RequestURL:    long,
	}.Normalize()

	checks := []struct {
		name  string
		value string
		limit int
	}{
		{"TraceID", rec.TraceID, MaxTraceID},
		{"Username", rec.Username, MaxUsername},
		{"UserIP", rec.UserIP, MaxIP},
		{"UserAgent", rec.UserAgent, MaxUserAgent},
		{"Module", rec.Module, MaxModule},
		{"SubModule", rec.SubModule, MaxSubModule},
		{"RequestMethod", rec.RequestMethod, MaxMethod},
		{"RequestURL", rec.RequestURL, MaxURL},
	}
	for _, c := range checks {
		if utf8.RuneCountInString(c.value) != c.limit {
			t.Fatalf("%s runes = %d, want %d", c.name, utf8.RuneCountInString(c.value), c.limit)
		}
	}
}

func TestRecordNormalizeKeepsUnboundedFields(t *testing.T) {
	// Contents/Extra 为 TEXT，不在截断契约内，原样透传。
	body := strings.Repeat("B", 5000)
	rec := Record{Contents: body, Extra: body}.Normalize()

	if rec.Contents != body {
		t.Fatalf("Contents was altered (len=%d)", len(rec.Contents))
	}
	if rec.Extra != body {
		t.Fatalf("Extra was altered (len=%d)", len(rec.Extra))
	}
}

func TestRecordNormalizePreservesUnknownUserType(t *testing.T) {
	rec := Record{UserType: UserType(42)}.Normalize()
	if rec.UserType != UserType(42) {
		t.Fatalf("UserType = %d, want unchanged 42", rec.UserType)
	}
}

func TestClipClientMeta(t *testing.T) {
	// 各字段造足够长的内容以真正触发截断（中文，验证按字符而非字节）。
	longCN := strings.Repeat("城", 100)     // 100 字符，超过 MaxGeo(64)/MaxIP(50)
	longUA := strings.Repeat("U", 600)      // 600 字符，超过 MaxUserAgent(512)
	meta := ClipClientMeta(ClientMeta{
		IP:          longCN,
		UserAgent:   longUA,
		GeoCountry:  longCN,
		GeoProvince: longCN,
		GeoCity:     longCN,
		GeoISP:      longCN,
	})

	if r := utf8.RuneCountInString(meta.IP); r != MaxIP {
		t.Fatalf("IP runes = %d, want %d", r, MaxIP)
	}
	if r := utf8.RuneCountInString(meta.UserAgent); r != MaxUserAgent {
		t.Fatalf("UserAgent runes = %d, want %d", r, MaxUserAgent)
	}
	for name, v := range map[string]string{
		"GeoCountry":  meta.GeoCountry,
		"GeoProvince": meta.GeoProvince,
		"GeoCity":     meta.GeoCity,
		"GeoISP":      meta.GeoISP,
	} {
		if r := utf8.RuneCountInString(v); r != MaxGeo {
			t.Fatalf("%s runes = %d, want %d", name, r, MaxGeo)
		}
		if !utf8.ValidString(v) {
			t.Fatalf("%s produced invalid UTF-8", name)
		}
	}
}

func TestNoopSinkAcceptsRecords(t *testing.T) {
	// Noop 是未装配时的安全默认值，必须可安全调用。
	Noop.Emit(t.Context(), Record{Module: "system"})
}

func TestSinkFuncAdapter(t *testing.T) {
	var got Record
	s := SinkFunc(func(_ context.Context, rec Record) { got = rec })

	s.Emit(t.Context(), Record{Module: "system", SubModule: "user"})

	if got.Module != "system" || got.SubModule != "user" {
		t.Fatalf("SinkFunc did not forward record: %+v", got)
	}
}

var _ Sink = Noop
var _ Sink = SinkFunc(nil)