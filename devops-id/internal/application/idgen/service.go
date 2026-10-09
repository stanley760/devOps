// Package idgen 提供发号用例的应用层入口。
//
// 当前是领域 Manager 之上的薄用例缝隙：gRPC 接口层依赖本包而非领域包，
// 参数校验与领域错误→gRPC 状态码的映射留在接口层（传输关注）。用例
// 扩展（配额、审计等）落在本层，不侵蚀领域模型。
package idgen

import (
	"context"

	iddomain "devops-id/internal/domain/idgen"
)

// Generator 发号器统一接口（应用层视角）。
type Generator interface {
	Next(ctx context.Context, bizTag string) (int64, error)
	BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error)
}

// Service 发号用例服务。
type Service struct {
	mgr *iddomain.Manager
}

// NewService 创建应用服务。
func NewService(mgr *iddomain.Manager) *Service {
	return &Service{mgr: mgr}
}

// Next 取单个 ID。
func (s *Service) Next(ctx context.Context, bizTag string) (int64, error) {
	return s.mgr.Next(ctx, bizTag)
}

// BatchNext 批量取 count 个严格递增 ID。
func (s *Service) BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error) {
	return s.mgr.BatchNext(ctx, bizTag, count)
}
