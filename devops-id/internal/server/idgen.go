// Package server 实现 IdGen gRPC 服务。
package server

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"devops-id/internal/snowflake"
	"devops-id/pkg/idgenpb"
)

// IdGenerator 发号器统一接口。
type IdGenerator interface {
	Next(ctx context.Context, bizTag string) (int64, error)
	BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error)
}

// maxBatchCount 单次批量取号上限。
const maxBatchCount = 100000

// IdGenServer IdGen gRPC 实现。
type IdGenServer struct {
	idgenpb.UnimplementedIdGenServer
	gen IdGenerator
}

// NewIdGenServer 创建实现。
func NewIdGenServer(gen IdGenerator) *IdGenServer {
	return &IdGenServer{gen: gen}
}

// Next 取单个 ID。
func (s *IdGenServer) Next(ctx context.Context, req *idgenpb.NextRequest) (*idgenpb.NextReply, error) {
	if req.GetBizTag() == "" {
		return nil, status.Error(codes.InvalidArgument, "biz_tag 不能为空")
	}
	id, err := s.gen.Next(ctx, req.GetBizTag())
	if err != nil {
		return nil, mapErr(err)
	}
	return &idgenpb.NextReply{Id: id}, nil
}

// BatchNext 原子分配 count 个 ID。
func (s *IdGenServer) BatchNext(ctx context.Context, req *idgenpb.BatchNextRequest) (*idgenpb.BatchNextReply, error) {
	if req.GetBizTag() == "" {
		return nil, status.Error(codes.InvalidArgument, "biz_tag 不能为空")
	}
	if req.GetCount() < 1 {
		return nil, status.Error(codes.InvalidArgument, "count 必须 >= 1")
	}
	if req.GetCount() > maxBatchCount {
		return nil, status.Errorf(codes.InvalidArgument, "count 超过单次上限 %d", maxBatchCount)
	}
	ids, err := s.gen.BatchNext(ctx, req.GetBizTag(), int(req.GetCount()))
	if err != nil {
		return nil, mapErr(err)
	}
	return &idgenpb.BatchNextReply{Ids: ids}, nil
}

// mapErr 把发号层错误映射到 gRPC 状态码：
// 参数非法 → InvalidArgument；时钟回拨 / lease 失效 → Unavailable；其余 → Internal。
func mapErr(err error) error {
	if errors.Is(err, snowflake.ErrInvalidName) {
		return status.Error(codes.InvalidArgument, "biz_tag 非法")
	}
	if errors.Is(err, snowflake.ErrClockBackward) {
		return status.Error(codes.Unavailable, "时钟回拨，请稍后重试")
	}
	if errors.Is(err, snowflake.ErrLeaseLost) {
		return status.Error(codes.Unavailable, "发号器 lease 失效（槽位可能被抢占），请稍后重试")
	}
	if errors.Is(err, snowflake.ErrManagerClosed) {
		return status.Error(codes.Unavailable, "发号器已关闭，请稍后重试")
	}
	return status.Error(codes.Internal, "发号失败")
}
