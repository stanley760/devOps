// Package types 跨层传输类型（DTO）。
//
// ID 契约（字符串化的 int64 主键）已下沉到 devops-common/types，
// 本包以类型别名转接，业务代码的 types.ID / types.ParamID 写法与
// 类型定义位置解耦：实现迁移时全仓调用点零改动。
package types

import (
	"errors"

	commontypes "github.com/stanley760/devops/devops-common/types"
	"github.com/stanley760/devops/devops-infra/errs"
)

// ID int64 主键的 DTO 形态：JSON 序列化为字符串（定义见 devops-common/types）。
type ID = commontypes.ID

// ParamID path/query 中的字符串形态 ID（定义见 devops-common/types）。
type ParamID = commontypes.ParamID

// InvalidIDError ID 参数解析失败（定义见 devops-common/types）。
type InvalidIDError = commontypes.InvalidIDError

// ParseID 解析路径/查询参数中的 ID（字符串形态）。
//
// 把零依赖层的 *InvalidIDError 映射为本服务的业务错误（400 参数错误），
// 使errs.ToHumaError 能正确识别——契约层不应反向依赖基础设施错误码。
func ParseID(s string) (ID, error) {
	id, err := commontypes.ParseID(s)
	if err != nil {
		return 0, asParamError(err)
	}
	return id, nil
}

// asParamError 将零依赖层的 ID 解析错误转换为 CodeParam 业务错误。
func asParamError(err error) error {
	var invalid *commontypes.InvalidIDError
	if errors.As(err, &invalid) {
		return errs.Wrap(err, errs.CodeParam, "ID 参数非法")
	}
	return err
}

// Parse 解析 path/query 中的 ID 并映射为业务错误。
//
// ParamID 是类型别名，不能直接给它加方法，故在此以包级函数提供同等能力；
// 调用点统一使用 types.ParseID(in.ID) 或本函数。
func Parse(id ParamID) (ID, error) { return ParseID(string(id)) }
