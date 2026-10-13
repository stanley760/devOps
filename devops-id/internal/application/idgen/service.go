// Package idgen 提供发号用例的应用层入口。
//
// 当前是领域 Manager 之上的薄用例缝隙：gRPC 接口层依赖本包而非领域包，
// 参数校验与领域错误→gRPC 状态码的映射留在接口层（传输关注）。用例
// 扩展（配额、审计等）落在本层，不侵蚀领域模型。
package idgen

import (
	"context"
	iddomain "github.com/stanley760/devOps/devops-id/internal/domain/idgen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"time"
)

// Generator 发号器统一接口（应用层视角）。
type Generator interface {
	Next(ctx context.Context, bizTag string) (int64, error)
	BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error)
}

// Service 发号用例服务。
type Service struct {
	mgr *iddomain.Manager

	batchLatency metric.Float64Histogram
}

// NewService 创建应用服务。
func NewService(mgr *iddomain.Manager) *Service {
	s := &Service{mgr: mgr}
	meter := otel.Meter("devops-id/idgen")
	h, err := meter.Float64Histogram(
		"idgen_batch_latency",
		metric.WithUnit("s"),
		metric.WithDescription("发号调用延迟（Next/BatchNext）"),
	)
	if err != nil {
		otel.Handle(err)
	}
	s.batchLatency = h
	return s
}

// Next 取单个 ID。
func (s *Service) Next(ctx context.Context, bizTag string) (int64, error) {
	startedAt := time.Now()
	id, err := s.mgr.Next(ctx, bizTag)
	s.recordLatency(ctx, bizTag, "next", time.Since(startedAt))
	return id, err
}

// BatchNext 批量取 count 个严格递增 ID（单调独占批次）。
func (s *Service) BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error) {
	startedAt := time.Now()
	ids, err := s.mgr.BatchNext(ctx, bizTag, count)
	s.recordLatency(ctx, bizTag, "batch_next", time.Since(startedAt))
	return ids, err
}

func (s *Service) recordLatency(ctx context.Context, bizTag, method string, d time.Duration) {
	if s.batchLatency == nil {
		return
	}
	s.batchLatency.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.String("biz_tag", bizTag),
		attribute.String("method", method),
	))
}
