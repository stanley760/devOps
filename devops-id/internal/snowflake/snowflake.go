// Package snowflake 实现美团 Leaf-snowflake（etcd 版）：本地 41bit 时间戳
// + 10bit workerID + 12bit 序列。
//
// etcd 能力映射：
//   - 顺序临时节点序号即 workerID → 槽位 key idgen/snowflake/{i}，依序扫描抢占
//   - 创建节点原子性 → Txn(If NotExists Then Put with lease)，raft 线性一致
//   - Session 心跳保活 → lease TTL + clientv3 自动 keepalive
//   - Session 过期节点删除 → lease 到期 etcd 自动删 key，槽位可被抢占
//   - 进程退出 ephemeral 消失 → Close 显式 revoke lease（优雅退出立即释放）
//
// 时钟回拨（对齐美团策略）：内存记录最近发号时间戳；回拨 < 5ms 自旋等待追平，
// ≥ 5ms 拒绝发号；启动时与本地持久化 last_timestamp 比对，异常回拨拒绝启动。
package snowflake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/etcd/client/v3"
)

const (
	workerBits = 10
	seqBits    = 12
	maxWorker  = -1 ^ (-1 << workerBits) // 1023
	maxSeq     = -1 ^ (-1 << seqBits)    // 4095

	// epoch 自定义纪元 2024-01-01T00:00:00Z（毫秒）：41bit 可用约 69 年。
	epoch = int64(1704067200000)

	slotPrefix = "idgen/snowflake/"

	// maxBackwardMs 时钟回拨容忍阈值（ms）：小于则自旋等待，大于则拒绝发号。
	maxBackwardMs = int64(5)
)

// ErrClockBackward 时钟回拨超过阈值（fail-fast，绝不冒重复 ID 风险）。
var ErrClockBackward = errors.New("时钟回拨超过阈值，拒绝发号")

// ErrLeaseLost lease 意外失效（非优雅退出：etcd 异常/网络分区致 TTL 到期）。
// 此时 worker 槽位可能已被其他实例抢占，继续发号会重号，宁可拒发。
var ErrLeaseLost = errors.New("lease 失效，worker 槽位可能被抢占，拒绝发号")

// Config snowflake 配置。
type Config struct {
	LeaseTTL    time.Duration // 槽位 lease TTL（宕机后槽位释放延迟）
	WorkerCache string        // 槽位/时间戳本地缓存目录（如 data/）
}

// Manager snowflake 发号器（实现 server.IdGenerator 接口，bizTag 被忽略）。
type Manager struct {
	mu            sync.Mutex
	lastTimestamp int64
	sequence      int64
	workerID      int64

	client         *clientv3.Client
	lease          clientv3.LeaseID
	kaCancel       context.CancelFunc
	leaseLost      atomic.Bool // lease 意外失效（非正常退出），此后拒绝发号
	normalShutdown atomic.Bool

	workerFile string // worker_id 槽位缓存
	tsFile     string // last_timestamp 持久化（启动回拨校验）
	closeOnce  sync.Once
}

// New 抢占 workerID 槽位并启动 keepalive（etcd 连接由调用方创建并管理生命周期）。
func New(ctx context.Context, client *clientv3.Client, cfg Config) (*Manager, error) {
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 10 * time.Second
	}
	dir := cfg.WorkerCache
	if dir == "" {
		dir = "data"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建缓存目录 %s 失败: %w", dir, err)
	}

	// lease + keepalive：TTL 到期（宕机未续约）槽位自动释放
	leaseResp, err := client.Grant(ctx, int64(cfg.LeaseTTL.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("创建 etcd lease 失败: %w", err)
	}
	kaCtx, kaCancel := context.WithCancel(context.Background())

	m := &Manager{
		client:     client,
		lease:      leaseResp.ID,
		kaCancel:   kaCancel,
		workerFile: filepath.Join(dir, "worker_id"),
		tsFile:     filepath.Join(dir, "last_timestamp"),
	}

	// KeepAlive 的响应通道必须排水，否则 etcd client 内部队列（容量 16）堆满后
	// 持续告警 "lease keepalive response queue is full"。
	kaCh, err := client.KeepAlive(kaCtx, leaseResp.ID)
	if err != nil {
		kaCancel()
		_, _ = client.Revoke(ctx, leaseResp.ID)
		return nil, fmt.Errorf("启动 lease keepalive 失败: %w", err)
	}
	go m.watchLease(kaCh)

	// 启动时钟回拨校验：当前时间不得明显早于上次持久化发号时间
	if persisted, ok := readIntFile(m.tsFile); ok && persisted > epoch {
		if now := time.Now().UnixMilli(); now < persisted-maxBackwardMs {
			m.releaseLease()
			return nil, fmt.Errorf(
				"系统时钟回拨异常：当前 %d 早于上次发号 %d（%dms），拒绝启动以防重号",
				now, persisted, persisted-now)
		}
	}

	// 槽位抢占：优先复用本地缓存槽位，失败则从 0 依序扫描
	workerID, err := m.acquireSlot(ctx)
	if err != nil {
		m.releaseLease()
		return nil, err
	}
	m.workerID = workerID
	m.lastTimestamp = time.Now().UnixMilli()

	// 定时持久化发号水位（宕机兜底；优雅退出由 Close 兜底）
	go m.persistLoop()

	slog.Info("snowflake 发号器就绪",
		"worker_id", workerID, "epoch_ms", epoch, "lease_ttl", cfg.LeaseTTL)
	return m, nil
}

// Next 生成一个 ID（bizTag 被忽略，snowflake 不分区业务）。
func (m *Manager) Next(ctx context.Context, bizTag string) (int64, error) {
	id, err := m.next()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// BatchNext 批量生成 count 个 ID（严格递增）。
func (m *Manager) BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error) {
	if count < 1 {
		return nil, fmt.Errorf("count 必须 >= 1: %d", count)
	}
	ids := make([]int64, count)
	for i := 0; i < count; i++ {
		id, err := m.next()
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// next 核心发号（位布局：| 41bit 毫秒 | 10bit worker | 12bit 序列 |）。
func (m *Manager) next() (int64, error) {
	if m.leaseLost.Load() {
		return 0, ErrLeaseLost
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UnixMilli()
	if now < m.lastTimestamp {
		diff := m.lastTimestamp - now
		if diff <= maxBackwardMs {
			// 小回拨：自旋等待时钟追平（带上限，防死循环）
			deadline := time.Now().Add(time.Duration(diff)*time.Millisecond + time.Second)
			for now < m.lastTimestamp {
				if time.Now().After(deadline) {
					return 0, fmt.Errorf("%w: 回拨 %dms 未追平", ErrClockBackward, diff)
				}
				now = time.Now().UnixMilli()
			}
		} else {
			return 0, fmt.Errorf("%w: 回拨 %dms", ErrClockBackward, diff)
		}
	}

	if now == m.lastTimestamp {
		m.sequence = (m.sequence + 1) & maxSeq
		if m.sequence == 0 { // 当前毫秒 4096 个序列耗尽，等下一毫秒
			for now <= m.lastTimestamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		m.sequence = 0
	}
	m.lastTimestamp = now

	return ((now - epoch) << (workerBits + seqBits)) | (m.workerID << seqBits) | m.sequence, nil
}

// Close 优雅退出：持久化发号水位 + 释放槽位（revoke lease 立即生效）。
// （宕机场景无 Close，靠 lease TTL 自动释放，期间槽位不可被抢占。）
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		last := m.lastTimestamp
		m.mu.Unlock()
		writeIntFile(m.tsFile, last)

		m.releaseLease()
		slog.Info("snowflake 已关闭：槽位释放", "worker_id", m.workerID)
	})
}

// releaseLease 停 keepalive 并 revoke lease 立即释放槽位（Close 与 New 失败路径共用）。
// 先标记正常退出再取消 keepalive 上下文，避免 watchLease 把通道关闭误判为 lease 失效。
func (m *Manager) releaseLease() {
	m.normalShutdown.Store(true)
	m.kaCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := m.client.Revoke(ctx, m.lease); err != nil {
		slog.Warn("revoke lease 失败（槽位将按 TTL 自动释放）", "err", err)
	}
}

// watchLease 排水 KeepAlive 响应通道并监听 lease 失效：
//   - 不排水会堆满 etcd client 内部 keepalive 队列（容量 16），
//     持续刷 "lease keepalive response queue is full" 告警
//   - 通道关闭即 lease 停止续期（etcd 异常/网络分区致 TTL 到期），
//     槽位将释放并被其他实例抢占，置 leaseLost 熔断发号（宁可拒发不可重号）
func (m *Manager) watchLease(kaCh <-chan *clientv3.LeaseKeepAliveResponse) {
	for range kaCh { // 排水：续期响应无需处理
	}
	if !m.normalShutdown.Load() {
		m.leaseLost.Store(true)
		slog.Error("lease 失效，发号器已熔断（worker 槽位将被抢占，请重启实例）",
			"worker_id", m.workerID)
	}
}

// acquireSlot 抢占 workerID 槽位：优先复用本地缓存（减少扫描），失败依序扫描 0-1023。
func (m *Manager) acquireSlot(ctx context.Context) (int64, error) {
	instance := instanceID()

	if cached, ok := readIntFile(m.workerFile); ok && cached >= 0 && cached <= maxWorker {
		if m.trySlot(ctx, int(cached), instance) {
			slog.Info("复用本地缓存 worker 槽位", "slot", cached)
			return int64(cached), nil
		}
		slog.Warn("缓存槽位被占用，重新扫描", "slot", cached)
	}

	for i := 0; i <= maxWorker; i++ {
		if m.trySlot(ctx, i, instance) {
			writeIntFile(m.workerFile, int64(i))
			return int64(i), nil
		}
	}
	return 0, errors.New("worker 槽位已满（0-1023 全部被占用），请下线实例或扩 worker 位")
}

// trySlot 原子抢占单个槽位：Txn(If NotExists Then Put with lease)。
func (m *Manager) trySlot(ctx context.Context, slot int, instance string) bool {
	key := fmt.Sprintf("%s%d", slotPrefix, slot)
	resp, err := m.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, instance, clientv3.WithLease(m.lease))).
		Commit()
	if err != nil {
		slog.Warn("抢占槽位失败", "slot", slot, "err", err)
		return false
	}
	return resp.Succeeded
}

// persistLoop 定时持久化发号水位（宕机后启动回拨校验依据）。
func (m *Manager) persistLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		last := m.lastTimestamp
		m.mu.Unlock()
		writeIntFile(m.tsFile, last)
	}
}

// instanceID 实例标识（写入槽位 value，便于排查持有者）。
func instanceID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

// readIntFile 读取整数缓存文件。
func readIntFile(path string) (int64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// writeIntFile 写入整数缓存文件（尽力而为，失败仅记日志）。
func writeIntFile(path string, v int64) {
	if err := os.WriteFile(path, []byte(strconv.FormatInt(v, 10)), 0o644); err != nil {
		slog.Warn("写缓存文件失败", "path", path, "err", err)
	}
}
