// Package types 跨层传输类型（DTO）。
//
// ID 统一以字符串下发（JSON number 精度丢失防护，为未来切 snowflake 19 位 ID 规范先行）。
package types

import (
	"fmt"
	"strconv"

	"github.com/stanley760/devOps/devops-infra/errs"
)

// ID int64 主键的 DTO 形态：JSON 序列化为字符串。
type ID int64

// ParamID keeps path and query IDs as strings until explicitly parsed,
// preventing clients from treating snowflake IDs as imprecise JSON numbers.
type ParamID string

// Parse converts a path or query ID into the internal numeric ID type.
func (i ParamID) Parse() (ID, error) { return ParseID(string(i)) }

// String 实现 Stringer。
func (i ID) String() string { return strconv.FormatInt(int64(i), 10) }

// Int64 还原为 int64。
func (i ID) Int64() int64 { return int64(i) }

// MarshalJSON 序列化为 JSON 字符串（带引号），避免前端 JS 精度丢失。
func (i ID) MarshalJSON() ([]byte, error) {
	return []byte(`"` + i.String() + `"`), nil
}

// UnmarshalJSON 接受带引号字符串（标准）或裸数字（宽松）。
func (i *ID) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*i = 0
		return nil
	}
	// 去引号
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("ID %q 非法: %w", s, err)
	}
	*i = ID(v)
	return nil
}

// UnmarshalText 支持 path/query 参数解析（huma 参数管线兜底 encoding.TextUnmarshaler）。
// 文档形态自动推导为 string（真实 JSON 形态）。
func (i *ID) UnmarshalText(b []byte) error {
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("ID %q 非法", string(b))
	}
	*i = ID(v)
	return nil
}

// ParseID 解析路径/查询参数中的 ID（字符串形态）。
func ParseID(s string) (ID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errs.New(errs.CodeParam, "ID 参数非法")
	}
	return ID(v), nil
}
