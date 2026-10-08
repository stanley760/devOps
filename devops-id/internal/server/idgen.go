// Package server 实现 IdGen gRPC 服务。
package server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
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
	startedAt := time.Now()
	id, err := s.gen.Next(ctx, req.GetBizTag())
	logCall(ctx, "next", req.GetBizTag(), 1, id, id, err, time.Since(startedAt))
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
	startedAt := time.Now()
	ids, err := s.gen.BatchNext(ctx, req.GetBizTag(), int(req.GetCount()))
	if err != nil {
		logCall(ctx, "batch_next", req.GetBizTag(), int(req.GetCount()), 0, 0, err, time.Since(startedAt))
		return nil, mapErr(err)
	}
	logCall(ctx, "batch_next", req.GetBizTag(), int(req.GetCount()), ids[0], ids[len(ids)-1], nil, time.Since(startedAt))
	return &idgenpb.BatchNextReply{Ids: ids}, nil
}

// logCall 落一行发号调用日志，补齐 infra AccessLog 缺失的业务语义：
// 谁（client）为哪个 biz_tag 取了多少号、耗时与结果号段（batch 严格递增，
// first/last 即号段边界）。与 AccessLog 分工——AccessLog 是「RPC 完成」标记
// （method/code/总耗时），本行是业务现场，排查「handler 卡死 vs 响应丢在
// 传输层」这类问题时两行拼出完整链路。成功 Debug（发号高频，不刷 info 级
// 日志）；失败 Warn（mapErr 只归到状态码，原始错误仅在此保留）。
// 携带 ctx：observability 的 loghandler 自动注入 trace_id/span_id，
// 可与调用方（devops-system）日志按 trace 互查。
func logCall(ctx context.Context, method, bizTag string, count int, firstID, lastID int64, err error, d time.Duration) {
	client := ""
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		client = p.Addr.String()
	}
	if err != nil {
		slog.WarnContext(ctx, "发号调用失败",
			"method", method, "biz_tag", bizTag, "count", count,
			"client", client, "err", err, "latency", d)
		return
	}
	slog.DebugContext(ctx, "发号调用",
		"method", method, "biz_tag", bizTag, "count", count,
		"first_id", firstID, "last_id", lastID, "client", client, "latency", d)
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
