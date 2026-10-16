package dao

import (
	"errors"
	"regexp"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// MySQL 唯一约束冲突错误码。
const (
	// errDupEntry 重复键（ER_DUP_ENTRY），单列唯一索引用。
	errDupEntry = 1062
	// errDupKey 唯一键冲突（ER_DUP_KEY），复合唯一索引用。
	errDupKey = 1586
)

// dupKeyIndexRE 从 MySQL 报错文本里提取冲突的索引名。
//
// MySQL 1062 的 Message 形如：
//
//	Duplicate entry 'shared@example.com' for key 't_user.uk_email'
//
// 故取 `for key '...'` 中的标识符，再按 `.` 切出末段（索引名）。
var dupKeyIndexRE = regexp.MustCompile("for key '([^']+)'")

// duplicateField 判断错误是否为唯一约束冲突，并返回冲突的**索引名**
// （uk_username / uk_email / uk_mobile），便于调用方给出准确文案。
// 解析不出索引名时返回空串。
//
// 为什么不用 gorm.ErrDuplicatedKey：devops-infra/database/mysql 的
// gorm.Open **没有开启 TranslateError**，该错误不会被翻译出来。
// 保留该判断作为前向兼容（万一 infra 未来开启翻译，此分支能先命中）。
func duplicateField(err error) (dup bool, indexName string) {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true, ""
	}
	var myErr *mysql.MySQLError
	if !errors.As(err, &myErr) {
		return false, ""
	}
	if myErr.Number != errDupEntry && myErr.Number != errDupKey {
		return false, ""
	}
	if m := dupKeyIndexRE.FindStringSubmatch(myErr.Message); len(m) == 2 {
		if i := strings.LastIndex(m[1], "."); i >= 0 {
			return true, m[1][i+1:]
		}
		return true, m[1]
	}
	return true, ""
}
