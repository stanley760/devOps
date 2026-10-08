// 登录审计 service 纯函数测试：parseIDsCSV 宽松解析 + 非法整体拒绝。
package service

import (
	"reflect"
	"testing"
)

func TestParseIDsCSV(t *testing.T) {
	got, err := parseIDsCSV("12, 34,56")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := []int64{12, 34, 56}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v, want %v", got, want)
	}

	if got, err := parseIDsCSV(""); err != nil || got != nil {
		t.Fatalf("空串应返回空集不报错: got=%v err=%v", got, err)
	}

	for _, bad := range []string{"abc", "12,,34", "-1", "1.5", "12,x"} {
		if _, err := parseIDsCSV(bad); err == nil {
			t.Fatalf("ids=%q 应整体拒绝", bad)
		}
	}
}
