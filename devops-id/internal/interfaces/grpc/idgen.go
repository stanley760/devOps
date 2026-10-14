// Package grpc 实现 IdGen gRPC 服务：传输层参数校验、调用应用服务、
// 领域错误→gRPC 状态码映射。
package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	idapp "github.com/stanley760/devops/devops-id/internal/application/idgen"
	iddomain "github.com/stanley760/devops/devops-id/internal/domain/idgen"
	"github.com/stanley760/devops/devops-id/pkg/idgenpb"
)

// maxBatchCount 单次批量取号上限（与领域层共享常量，防止漂移）。
const maxBatchCount = iddomain.DefaultMaxBatchCount

// IdGenServer IdGen gRPC 实现。
type IdGenServer struct {
	idgenpb.UnimplementedIdGenServer
	gen idapp.Generator
}

// NewIdGenServer 创建实现。
func NewIdGenServer(gen idapp.Generator) *IdGenServer {
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

// BatchNext 分配 count 个单调递增 ID（单调独占批次：批内严格递增，
// 无并发调用穿插，跨毫秒不连续）。
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
//   - context.Canceled / DeadlineExceeded → Canceled / DeadlineExceeded
//   - 参数非法（ErrInvalidName, ErrBatchTooLarge） → InvalidArgument
//   - 可重试的暂时性故障（时钟回拨 / lease 失效 / 槽位被占 / infra 不可用 /
//     无缓存槽位 / 管理器关闭） → Unavailable
//   - 资源耗尽（ErrSlotsExhausted, ErrTooManyTags） → ResourceExhausted
//   - 槽位映射损坏（需运维介入） → FailedPrecondition
//   - 其余 → Internal
func mapErr(err error) error {
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "请求已取消")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "请求超时")
	}
	if errors.Is(err, iddomain.ErrInvalidName) {
		return status.Error(codes.InvalidArgument, "biz_tag 非法")
	}
	if errors.Is(err, iddomain.ErrBatchTooLarge) {
		return status.Error(codes.InvalidArgument, "批量大小超过上限")
	}
	if errors.Is(err, iddomain.ErrClockBackward) {
		return status.Error(codes.Unavailable, "时钟回拨，请稍后重试")
	}
	if errors.Is(err, iddomain.ErrLeaseLost) {
		return status.Error(codes.Unavailable, "发号器 lease 失效（槽位可能被抢占），请稍后重试")
	}
	if errors.Is(err, iddomain.ErrManagerClosed) {
		return status.Error(codes.Unavailable, "发号器已关闭，请稍后重试")
	}
	if errors.Is(err, iddomain.ErrSlotOccupied) {
		return status.Error(codes.Unavailable, "worker 槽位被占用，请稍后重试")
	}
	if errors.Is(err, iddomain.ErrInfraUnavailable) {
		return status.Error(codes.Unavailable, "发号基础设施不可用，请稍后重试")
	}
	if errors.Is(err, iddomain.ErrNoCachedSlot) {
		return status.Error(codes.Unavailable, "发号器降级不可用（无缓存槽位），请稍后重试")
	}
	if errors.Is(err, iddomain.ErrSlotsExhausted) {
		return status.Error(codes.ResourceExhausted, "worker 槽位已耗尽")
	}
	if errors.Is(err, iddomain.ErrTooManyTags) {
		return status.Error(codes.ResourceExhausted, "biz_tag 数量已达上限")
	}
	if errors.Is(err, iddomain.ErrSlotMappingInvalid) {
		return status.Error(codes.FailedPrecondition, "槽位映射损坏，请联系运维")
	}
	return status.Error(codes.Internal, "发号失败")
}
