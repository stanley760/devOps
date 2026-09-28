// Package idclient 是 devops-id 发号服务的客户端 SDK（预取缓冲）。
//
// 关键设计：把「每 ID 一次 RPC」摊薄到 1/batch——进程内持本地预取缓冲，
// 一次 BatchNext 取回一批（默认 100），本地零网络发号：
//   - devops-id 重启不影响已预取的号（号已原子分配到本进程，弹性窗口 = batch）
//   - 剩余低于水位 → 异步单飞补充
//   - 缓冲耗尽且补充失败 → fail-fast 返回错误（同服务端哲学：宁可拒发不可重号）
//
// 多副本消费方各持独立缓冲：同一 biz_tag 内 ID 趋势递增，但跨副本交错。
package idclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

	metrics *bufferMetrics
}

// tagBuffer 单个 biz_tag 的缓冲状态。
// attrs 为预构建的 biz_tag 属性集：发号热路径零属性分配。
type tagBuffer struct {
	mu      sync.Mutex
	ids     []int64
	loading bool // 异步补充单飞去重
	attrs   metric.MeasurementOption
}

// bufferMetrics 发号缓冲指标集。仪表创建失败时 OTel 返回 noop 仪表（非 nil），
// 观测故障不影响发号。
type bufferMetrics struct {
	remaining  metric.Int64ObservableGauge
	capacity   metric.Int64ObservableGauge
	issued     metric.Int64Counter
	refillFail metric.Int64Counter
	refillDur  metric.Float64Histogram
}

// New 创建发号客户端。conn 由调用方创建（devops-infra/grpc.Dial）并管理生命周期。
// 指标经全局 MeterProvider 导出：服务方应在调用 New 前完成 observability.Init；
// 未初始化时（探针/测试）自动落到 noop 仪表，无任何开销副作用。
func New(conn *grpc.ClientConn, cfg BufferConfig) *Client {
	if cfg.Batch < 1 {
		cfg.Batch = 1
	}
	if cfg.LowWater <= 0 {
		cfg.LowWater = 0.2
	}
	c := &Client{
		conn:    conn,
		stub:    idgenpb.NewIdGenClient(conn),
		health:  healthpb.NewHealthClient(conn),
		cfg:     cfg,
		buffers: make(map[string]*tagBuffer),
	}
	c.initMetrics()
	return c
}

// initMetrics 创建缓冲指标并注册抓取时回调（水位 gauge 无需热路径写）。
// 回调持锁读取各缓冲剩余量：Next 空缓冲同步补充时最长阻塞 CallTimeout，
// 抓取因此可能短暂等锁，量级（秒）远小于 Prometheus 抓取超时，可接受。
func (c *Client) initMetrics() {
	meter := otel.Meter("devops-id/idclient")
	m := &bufferMetrics{}
	var err error
	if m.remaining, err = meter.Int64ObservableGauge(
		"idgen_buffer_remaining",
		metric.WithDescription("发号本地预取缓冲剩余号量（0 = 耗尽，下一次发号将同步 RPC）"),
	); err != nil {
		otel.Handle(err)
	}
	if m.capacity, err = meter.Int64ObservableGauge(
		"idgen_buffer_capacity",
		metric.WithDescription("发号预取单批容量（BufferConfig.Batch）"),
	); err != nil {
		otel.Handle(err)
	}
	if m.issued, err = meter.Int64Counter(
		"idgen_issued",
		metric.WithDescription("本地缓冲发出的 ID 累计数"),
	); err != nil {
		otel.Handle(err)
	}
	if m.refillFail, err = meter.Int64Counter(
		"idgen_refill_failures",
		metric.WithDescription("缓冲补充失败次数；trigger=sync 表示已阻塞业务请求，async 为后台预取失败"),
	); err != nil {
		otel.Handle(err)
	}
	if m.refillDur, err = meter.Float64Histogram(
		"idgen_refill_duration",
		metric.WithUnit("s"),
		metric.WithDescription("BatchNext 补充耗时"),
	); err != nil {
		otel.Handle(err)
	}
	c.metrics = m

	if _, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		type entry struct {
			tag string
			tb  *tagBuffer
		}
		c.mu.Lock()
		entries := make([]entry, 0, len(c.buffers))
		for tag, tb := range c.buffers {
			entries = append(entries, entry{tag, tb})
		}
		c.mu.Unlock()
		for _, e := range entries {
			e.tb.mu.Lock()
			n := len(e.tb.ids)
			e.tb.mu.Unlock()
			attrs := metric.WithAttributes(attribute.String("biz_tag", e.tag))
			o.ObserveInt64(m.remaining, int64(n), attrs)
			o.ObserveInt64(m.capacity, int64(c.cfg.Batch), attrs)
		}
		return nil
	}, m.remaining, m.capacity); err != nil {
		otel.Handle(err)
	}
}

// Next 从本地预取缓冲取一个 ID。
// bizTag 用于选择独立的 snowflake worker 空间。
func (c *Client) Next(ctx context.Context, bizTag string) (int64, error) {
	tb := c.get(bizTag)
	tb.mu.Lock()

	for {
		if len(tb.ids) > 0 {
			// 从头部弹出：服务端返回的批内单调递增，头部弹出保证本地发号也递增
			id := tb.ids[0]
			tb.ids = tb.ids[1:]
			c.metrics.recordIssued(ctx, tb.attrs)
			c.asyncRefill(tb, bizTag)
			tb.mu.Unlock()
			return id, nil
		}

		// 缓冲空：同步补充（batch=1 即逐次 RPC）
		ids, err := c.fetchTracked(ctx, bizTag, "sync")
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
		tb = &tagBuffer{
			attrs: metric.WithAttributes(attribute.String("biz_tag", bizTag)),
		}
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
		ids, err := c.fetchTracked(context.Background(), bizTag, "async")
		if err != nil {
			return // 补充失败不打断发号：缓冲耗尽时 Next 会同步重试
		}
		tb.mu.Lock()
		defer tb.mu.Unlock()
		tb.ids = append(tb.ids, ids...)
	}()
}

// fetchTracked 计时并记录补充结果指标（成功记耗时，失败记耗时+失败计数）。
func (c *Client) fetchTracked(ctx context.Context, bizTag, trigger string) ([]int64, error) {
	startedAt := time.Now()
	ids, err := c.fetch(ctx, bizTag)
	if c.metrics != nil {
		c.metrics.recordRefill(ctx, bizTag, trigger, time.Since(startedAt), err != nil)
	}
	return ids, err
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

// recordIssued 热路径：使用 tagBuffer 预构建属性集，零分配记录发号数。
func (m *bufferMetrics) recordIssued(ctx context.Context, attrs metric.MeasurementOption) {
	if m == nil {
		return
	}
	m.issued.Add(ctx, 1, attrs)
}

// recordRefill 冷路径（补充才发生）：失败计数 + 耗时直方图。
func (m *bufferMetrics) recordRefill(ctx context.Context, bizTag, trigger string, d time.Duration, failed bool) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("biz_tag", bizTag),
		attribute.String("trigger", trigger),
	)
	m.refillDur.Record(ctx, d.Seconds(), attrs)
	if failed {
		m.refillFail.Add(ctx, 1, attrs)
	}
}
