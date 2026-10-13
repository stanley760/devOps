package domain

import (
	"strconv"
	"testing"
)

func TestPageParamNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   PageParam
		want PageParam
	}{
		{
			name: "defaults",
			want: PageParam{PageNum: DefaultPageNum, PageSize: DefaultPageSize},
		},
		{
			name: "clamps page size",
			in:   PageParam{PageNum: 3, PageSize: MaxPageSize + 1},
			want: PageParam{PageNum: 3, PageSize: MaxPageSize},
		},
		{
			name: "replaces invalid values",
			in:   PageParam{PageNum: -1, PageSize: -1},
			want: PageParam{PageNum: DefaultPageNum, PageSize: DefaultPageSize},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalize(); got != tt.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPageParamOffset(t *testing.T) {
	if got, want := (PageParam{PageNum: 3, PageSize: 20}).Offset(), 40; got != want {
		t.Fatalf("Offset() = %d, want %d", got, want)
	}

	maxInt := int(^uint(0) >> 1)
	got := (PageParam{PageNum: maxInt, PageSize: MaxPageSize}).Offset()
	if got != maxInt {
		t.Fatalf("Offset() overflow = %d, want saturated value %d", got, maxInt)
	}
}

func TestMapPagePreservesPaginationMetadata(t *testing.T) {
	page := PageResult[int]{
		List:     []int{1, 2},
		Total:    25,
		PageNum:  2,
		PageSize: 10,
		Pages:    3,
	}

	got := MapPage(page, func(value int) string {
		return strconv.Itoa(value)
	})
	want := PageResult[string]{
		List:     []string{"1", "2"},
		Total:    page.Total,
		PageNum:  page.PageNum,
		PageSize: page.PageSize,
		Pages:    page.Pages,
	}
	if len(got.List) != len(want.List) {
		t.Fatalf("MapPage() list length = %d, want %d", len(got.List), len(want.List))
	}
	for i := range want.List {
		if got.List[i] != want.List[i] {
			t.Fatalf("MapPage() list[%d] = %q, want %q", i, got.List[i], want.List[i])
		}
	}
	if got.Total != want.Total || got.PageNum != want.PageNum ||
		got.PageSize != want.PageSize || got.Pages != want.Pages {
		t.Fatalf("MapPage() metadata = %+v, want %+v", got, want)
	}
}
