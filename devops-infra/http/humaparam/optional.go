// Package humaparam huma 请求参数辅助类型（D7）。
//
// huma 参数管线不支持指针类型（form/header/path/query 会 panic，huma.go:215），
// 可选语义经 ParamWrapper/ParamReactor 接口实现：解析写入 Value、出现与否记录 IsSet。
package humaparam

import "reflect"

// Optional 可选参数包装：区分「未传」与「显式零值」。
//
//	Input 内声明：Status humaparam.Optional[int8] `query:"status" minimum:"0" maximum:"1"`
//	handler 侧转换：svc.List(ctx, in.Status.Ptr()) —— 未传为 nil，与 service 层 *int8 语义对齐。
type Optional[T any] struct {
	Value T
	IsSet bool
}

// Receiver 实现 huma.ParamWrapper：暴露接收解析值的字段。
func (o *Optional[T]) Receiver() reflect.Value {
	return reflect.ValueOf(o).Elem().Field(0)
}

// OnParamSet 实现 huma.ParamReactor：记录参数是否在请求中出现。
func (o *Optional[T]) OnParamSet(isSet bool, _ any) {
	o.IsSet = isSet
}

// Ptr 适配 service 层指针语义：未传返回 nil，已传返回指向值的指针。
func (o *Optional[T]) Ptr() *T {
	if !o.IsSet {
		return nil
	}
	v := o.Value
	return &v
}
