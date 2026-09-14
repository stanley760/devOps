// Package idclient 是 devops-id 发号服务的客户端 SDK（预取缓冲）。
//
// 关键设计：把「每 ID 一次 RPC」摊薄到 1/batch——进程内持本地预取缓冲，
// 一次 BatchNext 取回一批（默认 100），本地零网络发号：
//   - devops-id 重启不影响已预取的号（号已原子分配到本进程，弹性窗口 = batch）
//   - 剩余低于水位 → 异步单飞补充
//   - 缓冲耗尽且补充失败 → fail-fast 返回错误（同服务端哲学：宁可拒发不可重号）
//
// 多副本消费方各持独立缓冲：ID 趋势递增但跨副本交错（与 segment 语义一致）。
package idclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"devops-id/pkg/idgenpb"
)

// BufferConfig 预取缓冲配置。
type BufferConfig struct {
	Batch       int           // 预取批量（1 = 关闭预取，逐次 RPC）
	LowWater    float64       // 剩余比例低于该值时异步补充
	CallTimeout time.Duration // fetch 单次调用超时（0 = 不限时，跟随传入 ctx）
}

// Client 发号客户端：一个 gRPC 连接 + 按 biz_tag 的本地预取缓冲。
type Client struct {
	conn   *grpc.ClientConn
	stub   idgenpb.IdGenClient
	health healthpb.HealthClient
	cfg    BufferConfig

	mu      sync.Mutex
	buffers map[string]*tagBuffer
}

// tagBuffer 单个 biz_tag 的缓冲状态。
type tagBuffer struct {
	mu      sync.Mutex
	ids     []int64
	loading bool // 异步补充单飞去重
}

// New 创建发号客户端。conn 由调用方创建（devops-infra/grpc.Dial）并管理生命周期。
func New(conn *grpc.ClientConn, cfg BufferConfig) *Client {
	if cfg.Batch < 1 {
		cfg.Batch = 1
	}
	if cfg.LowWater <= 0 {
		cfg.LowWater = 0.2
	}
	return &Client{
		conn:    conn,
		stub:    idgenpb.NewIdGenClient(conn),
		health:  healthpb.NewHealthClient(conn),
		cfg:     cfg,
		buffers: make(map[string]*tagBuffer),
	}
}

// Next 从本地预取缓冲取一个 ID。
// bizTag 必须已在 devops-id 侧 t_leaf_alloc 注册（消费方 migrate 负责）。
func (c *Client) Next(ctx context.Context, bizTag string) (int64, error) {
	tb := c.get(bizTag)
	tb.mu.Lock()

	for {
		if len(tb.ids) > 0 {
			// 从头部弹出：服务端返回的批内单调递增，头部弹出保证本地发号也递增
			id := tb.ids[0]
			tb.ids = tb.ids[1:]
			c.asyncRefill(tb, bizTag)
			tb.mu.Unlock()
			return id, nil
		}

		// 缓冲空：同步补充（batch=1 即逐次 RPC）
		ids, err := c.fetch(ctx, bizTag)
		if err != nil {
			tb.mu.Unlock()
			return 0, fmt.Errorf("ID 生成失败(biz_tag=%s): %w", bizTag, err)
		}
		tb.ids = ids
	}
}

// get 取（或惰性创建）biz_tag 缓冲。
func (c *Client) get(bizTag string) *tagBuffer {
	c.mu.Lock()
	defer c.mu.Unlock()
	tb, ok := c.buffers[bizTag]
	if !ok {
		tb = &tagBuffer{}
		c.buffers[bizTag] = tb
	}
	return tb
}

// asyncRefill 剩余低于水位时异步补充（调用方持有 tb.mu）。
func (c *Client) asyncRefill(tb *tagBuffer, bizTag string) {
	if c.cfg.Batch <= 1 { // 预取关闭
		return
	}
	if len(tb.ids) >= int(float64(c.cfg.Batch)*c.cfg.LowWater) {
		return
	}
	if tb.loading {
		return
	}
	tb.loading = true
	go func() {
		defer func() {
			tb.mu.Lock()
			tb.loading = false
			tb.mu.Unlock()
		}()
		ids, err := c.fetch(context.Background(), bizTag)
		if err != nil {
			return // 补充失败不打断发号：缓冲耗尽时 Next 会同步重试
		}
		tb.mu.Lock()
		defer tb.mu.Unlock()
		tb.ids = append(tb.ids, ids...)
	}()
}

// fetch 调用 BatchNext 取一批号（带 CallTimeout 限时）。
func (c *Client) fetch(ctx context.Context, bizTag string) ([]int64, error) {
	if c.cfg.CallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.CallTimeout)
		defer cancel()
	}
	reply, err := c.stub.BatchNext(ctx, &idgenpb.BatchNextRequest{
		BizTag: bizTag,
		Count:  int32(c.cfg.Batch),
	})
	if err != nil {
		return nil, err
	}
	return reply.GetIds(), nil
}

// Ping 探活：gRPC 标准健康检查（SERVING 状态）。
func (c *Client) Ping(ctx context.Context) error {
	reply, err := c.health.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if reply.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("devops-id 健康状态异常: %s", reply.GetStatus())
	}
	return nil
}
