// Package server 实现 IdGen gRPC 服务。
package server

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"devops-id/internal/segment"
	"devops-id/pkg/idgenpb"
)

// IdGenServer IdGen gRPC 实现：委托给 segment 管理器。
// snowflake 模式随 P9 交付，届时按配置装配。
type IdGenServer struct {
	idgenpb.UnimplementedIdGenServer
	mgr *segment.Manager
}

// NewIdGenServer 创建实现。
func NewIdGenServer(mgr *segment.Manager) *IdGenServer {
	return &IdGenServer{mgr: mgr}
}

// Next 取单个 ID。
func (s *IdGenServer) Next(ctx context.Context, req *idgenpb.NextRequest) (*idgenpb.NextReply, error) {
	if req.GetBizTag() == "" {
		return nil, status.Error(codes.InvalidArgument, "biz_tag 不能为空")
	}
	id, err := s.mgr.Next(ctx, req.GetBizTag())
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
	ids, err := s.mgr.BatchNext(ctx, req.GetBizTag(), int(req.GetCount()))
	if err != nil {
		return nil, mapErr(err)
	}
	return &idgenpb.BatchNextReply{Ids: ids}, nil
}

// mapErr 把发号层错误映射到 gRPC 状态码：
// 号段耗尽 → Unavailable（建议重试）；tag 未注册 → NotFound；其余 → Internal。
func mapErr(err error) error {
	if errors.Is(err, segment.ErrSegmentExhausted) {
		return status.Error(codes.Unavailable, "号段耗尽且回源失败，请稍后重试")
	}
	if strings.Contains(err.Error(), "未注册") {
		return status.Error(codes.NotFound, "biz_tag 未注册")
	}
	return status.Error(codes.Internal, "发号失败")
}
