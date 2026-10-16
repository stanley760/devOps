package dao

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
)

// TestDuplicateField_RealMySQLMessage 用真实 MySQL 报错文本验证索引名解析。
//
// 依据实测（devops-user 接入 GORM 时抓到的真实日志）：
//
//	Error 1062 (23000): Duplicate entry 'shared@example.com' for key 't_user.uk_email'
func TestDuplicateField_RealMySQLMessage(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    string
	}{
		{"email", "Duplicate entry 'shared@example.com' for key 't_user.uk_email'", "uk_email"},
		{"mobile", "Duplicate entry '13900000001' for key 't_user.uk_mobile'", "uk_mobile"},
		{"username", "Duplicate entry 'alice' for key 't_user.uk_username'", "uk_username"},
		{"无表前缀", "Duplicate entry 'x' for key 'uk_email'", "uk_email"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dup, index := duplicateField(&mysql.MySQLError{Number: errDupEntry, Message: c.message})
			if !dup {
				t.Fatal("dup = false, want true")
			}
			if index != c.want {
				t.Fatalf("index = %q, want %q", index, c.want)
			}
		})
	}
}

// TestDuplicateField_ThroughWrapping 错误经包装后仍能解析出索引名。
//
// GORM 返回的错误可能带上下文，必须保证 errors.As 能穿透。
func TestDuplicateField_ThroughWrapping(t *testing.T) {
	base := &mysql.MySQLError{Number: errDupEntry, Message: "Duplicate entry 'a@b.com' for key 't_user.uk_email'"}

	if _, index := duplicateField(fmt.Errorf("插入失败: %w", base)); index != "uk_email" {
		t.Fatalf("fmt.Errorf 包装后 index = %q", index)
	}
	if _, index := duplicateField(errors.Join(errors.New("外层"), base)); index != "uk_email" {
		t.Fatalf("errors.Join 后 index = %q", index)
	}
}

// TestDuplicateField_NotDuplicate 非唯一约束错误不应误判。
func TestDuplicateField_NotDuplicate(t *testing.T) {
	cases := []error{
		errors.New("普通错误"),
		&mysql.MySQLError{Number: 1146, Message: "Table 'devops.t_user' doesn't exist"},
	}
	for _, err := range cases {
		if dup, _ := duplicateField(err); dup {
			t.Fatalf("err=%v 不应判为重复键", err)
		}
	}
}

// TestDuplicateField_UnknownIndex 冲突但解析不出索引名时，dup 为 true、索引为空。
//
// 此时调用方会走兜底文案（不泄露细节），这是期望行为。
func TestDuplicateField_UnknownIndex(t *testing.T) {
	dup, index := duplicateField(&mysql.MySQLError{Number: errDupEntry, Message: "Duplicate entry 'x'"})
	if !dup {
		t.Fatal("dup = false, want true")
	}
	if index != "" {
		t.Fatalf("index = %q, want 空串", index)
	}
}
