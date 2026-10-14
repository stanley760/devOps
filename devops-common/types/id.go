// Package types 提供跨层传输的基础类型。
//
// 本包保持零外部依赖（仅标准库）：ID 的字符串化是跨业务契约，
// 不应绑定任何基础设施（错误码、日志、存储等）。
package types

import (
	"fmt"
	"strconv"
)

// ID int64 主键的 DTO 形态：JSON 序列化为字符串。
//
// 为未来切snowflake 19 位 ID 规范先行：JSON number 在 JS 中超过 2^53
// 会精度丢失，故线上契约统一以字符串下发。
type ID int64

// ParamID keeps path and query IDs as strings until explicitly parsed,
// preventing clients from treating snowflake IDs as imprecise JSON numbers.
type ParamID string

// InvalidIDError ID 参数解析失败。
//
// 刻意不绑定基础设施错误码（errs）：本包须保持零外部依赖，
// 由各业务的错误映射层将其转成对应的业务码。
type InvalidIDError struct {
	Value string
	Cause error
}

func (e *InvalidIDError) Error() string {
	return fmt.Sprintf("ID 参数非法: %q", e.Value)
}

// Unwrap 支持 errors.Is / errors.As 穿透到 strconv 的底层错误。
func (e *InvalidIDError) Unwrap() error { return e.Cause }

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
// 失败返回 *InvalidIDError，由调用方的错误映射层决定响应码。
func ParseID(s string) (ID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, &InvalidIDError{Value: s, Cause: err}
	}
	return ID(v), nil
}