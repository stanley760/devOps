package iam

import (
	"encoding/json"
	"strconv"
	"strings"
)

// 存储层 ID 数组的 JSON 编解码。
//
// 这是一个不可变的存储契约（存量数据 + 唯一索引 + SQL 数字匹配口径依赖它）：
//
//	合法形态：[4,5]          数字数组
//	脏数据：  ["4","5"]      字符串元素 → 视为脏数据，整体解析失败返回空集
//
// 为什么不能直接 marshal []types.ID：types.ID 是 API 契约类型，其 MarshalJSON
// 输出字符串元素（["4","5"]，前端契约要求 ID 字符串化以避免 JS 2^53 精度丢失）。
// 若直接复用会把 API 形态泄漏进存储层，与种子数据及
// JSON_CONTAINS(post_ids, 4) 的数字匹配口径不一致。
//
// 改造前该逻辑在 dao/user_admin.go:209-241（PostIDsOf / PostIDsJSON），
// 且因为 dao 反向依赖 internal/types 才不得不手写这套转换。领域层不认识
// API 契约类型后，编解码自然归位于此。

// EncodeIDArray 数字 ID 切片 → 存储 JSON 字符串。空集返回 ""（由仓储决定写 NULL）。
func EncodeIDArray(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	// 复制一份并剔除 nil 语义：空切片会被 marshal 成 [] 而非 null。
	b, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeIDArray 存储 JSON 字符串 → 数字 ID 切片。
//
// 容错口径（与既有实现逐字对齐）：
//   - 空串 → 空切片（非 nil）
//   - 非法 JSON 或含非数字元素 → 空切片，不做兼容读
func DecodeIDArray(raw string) []int64 {
	if raw == "" {
		return []int64{}
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []int64{}
	}
	if ids == nil {
		return []int64{}
	}
	return ids
}

// decodeInt64JSONArray 是 DecodeIDArray 的宽松变体：非法输入返回 nil 而非空切片，
// 用于「自定义数据权限部门集合」这类「非法即视为未配置」的判定场景。
func decodeInt64JSONArray(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	return ids
}

// NormalizeIDArrayJSON 归一化为数字数组形态（落库前统一口径）。
//
// 改造前该归一化由 devops-infra/datascope.NormalizeDeptIDs 承担角色部门集合，
// 此处为同构实现，使岗位集合（post_ids）也能复用同一口径。
func NormalizeIDArrayJSON(raw string) string {
	return EncodeIDArray(DecodeIDArray(raw))
}

// IDsFromStrings 从字符串数字列表转 int64 切片（非法项跳过）。
func IDsFromStrings(raw []string) []int64 {
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}
