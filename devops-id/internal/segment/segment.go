// Package segment 实现美团 Leaf-segment 号段模式（服务端双 buffer + 动态 step）。
//
// 核心机制：
//   - 号段从 MySQL t_leaf_alloc 批量取回内存（UPDATE 推进 max_id + 回读），
//     行锁天然保证多实例并发取号段互斥
//   - 双 buffer：cur 当前号段发号，剩余低于水位时异步预加载 next；
//     DB 短暂不可用仍可发号（Leaf 解耦 DB 可用性的核心价值）
//   - 双 buffer 耗尽且加载失败 → fail-fast 拒发（宁可拒发不可重号）
//   - 动态 step（对齐 Leaf 进阶特性）：按号段消耗速率（两次回源间隔）调整步长，
//     消耗快则翻倍防频繁回源，消耗慢则减半省号；调整值写回 t_leaf_alloc 跨实例生效
//   - 服务重启丢号段尾部：趋势递增、非严格连续（业务可接受）
package segment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrSegmentExhausted 号段耗尽且回源失败（fail-fast，建议向客户端返回 Unavailable）。
var ErrSegmentExhausted = errors.New("号段耗尽且加载失败")

// Alloc 号段分配表（t_leaf_alloc），由消费方 migrate 负责注册行。
type Alloc struct {
	BizTag      string `gorm:"column:biz_tag;primaryKey;size:128"`
	MaxID       int64  `gorm:"column:max_id;not null;default:1"`
	Step        int    `gorm:"column:step;not null;default:1000"`
	Description string `gorm:"column:description;size:256"`
}

// TableName 表名（对齐美团 leaf_alloc 原名，t_ 前缀）。
func (Alloc) TableName() string { return "t_leaf_alloc" }

// buffer 内存号段 (start, end]。
type buffer struct {
	start int64
	end   int64
	pos   int64 // 下一个待发号
}

// remain 剩余可发数量。
func (b *buffer) remain() int64 { return b.end - b.pos + 1 }

// Config 号段模式配置。
type Config struct {
	Step     int     // 号段长度（与 t_leaf_alloc.step 对齐，默认 1000）
	LowWater float64 // 预加载水位（剩余 < step × low_water 时异步加载下一号段）
}

// Manager 管理所有 biz_tag 的双 buffer 号段。
type Manager struct {
	db  *gorm.DB
	cfg Config

	mu      sync.Mutex
	buffers map[string]*segmentBuffer
}

// 动态 step 调整参数（对齐 Leaf 默认值）：
// 两次回源间隔即号段消耗时长；消耗快于 15min 翻倍、慢于 30min 减半，步长范围 [100, 50000]。
const (
	stepGrowBelow   = 15 * time.Minute
	stepShrinkAbove = 30 * time.Minute
	stepMin         = int64(100)
	stepMax         = int64(50000)
)

// segmentBuffer 单个 biz_tag 的双 buffer 状态。
type segmentBuffer struct {
	mu      sync.Mutex
	cur     *buffer
	next    *buffer // 预加载号段（双 buffer 核心）
	loading bool    // 异步加载单飞去重

	curStep    int64     // 动态 step 当前值（回源时调整并写回 t_leaf_alloc）
	lastLoadAt time.Time // 上次回源时间（与本次回源间隔 = 号段消耗时长）
}

// New 创建号段管理器。
func New(db *gorm.DB, cfg Config) *Manager {
	if cfg.Step <= 0 {
		cfg.Step = 1000
	}
	if cfg.LowWater <= 0 {
		cfg.LowWater = 0.1
	}
	return &Manager{
		db:      db,
		cfg:     cfg,
		buffers: make(map[string]*segmentBuffer),
	}
}

// Next 从指定 biz_tag 取一个 ID。
func (m *Manager) Next(ctx context.Context, bizTag string) (int64, error) {
	ids, err := m.nextN(ctx, bizTag, 1)
	if err != nil {
		return 0, err
	}
	return ids[0], nil
}

// BatchNext 原子分配 count 个 ID（单调递增）。
func (m *Manager) BatchNext(ctx context.Context, bizTag string, count int) ([]int64, error) {
	if count < 1 {
		return nil, fmt.Errorf("count 必须 >= 1: %d", count)
	}
	return m.nextN(ctx, bizTag, count)
}

// nextN 核心取号逻辑：当前号段够则直接取；不足则尝试切换 next；仍不足则同步加载。
func (m *Manager) nextN(ctx context.Context, bizTag string, count int) ([]int64, error) {
	if bizTag == "" {
		return nil, errors.New("biz_tag 不能为空")
	}

	sb := m.get(bizTag)
	sb.mu.Lock()
	defer sb.mu.Unlock()

	for {
		if sb.cur != nil && sb.cur.remain() >= int64(count) {
			// 当前号段足够，扣减发号
			ids := make([]int64, count)
			for i := 0; i < count; i++ {
				sb.cur.pos++
				ids[i] = sb.cur.pos
			}
			m.asyncLoadIfNeeded(sb, bizTag)
			return ids, nil
		}

		// 当前不够：优先切换已预加载的 next（双 buffer 兜底，DB 不可用也能发）
		if sb.next != nil {
			sb.cur = sb.next
			sb.next = nil
			continue
		}

		// next 也没有：同步回源加载（返回错误则 fail-fast；号段按 count 放大防死循环）
		nb, nstep, err := m.load(ctx, bizTag, sb.curStep, sinceLastLoad(sb), count)
		if err != nil {
			return nil, fmt.Errorf("%w: biz_tag=%s: %v", ErrSegmentExhausted, bizTag, err)
		}
		sb.curStep = nstep
		sb.lastLoadAt = time.Now()
		sb.cur = nb
	}
}

// sinceLastLoad 距上次回源的间隔（首次为 0；调用方持有 sb.mu）。
func sinceLastLoad(sb *segmentBuffer) time.Duration {
	if sb.lastLoadAt.IsZero() {
		return 0
	}
	return time.Since(sb.lastLoadAt)
}

// get 取（或惰性创建）biz_tag 对应的 segmentBuffer。
func (m *Manager) get(bizTag string) *segmentBuffer {
	m.mu.Lock()
	defer m.mu.Unlock()
	sb, ok := m.buffers[bizTag]
	if !ok {
		sb = &segmentBuffer{}
		m.buffers[bizTag] = sb
	}
	return sb
}

// asyncLoadIfNeeded 剩余低于水位时异步预加载下一号段（调用方持有 sb.mu）。
func (m *Manager) asyncLoadIfNeeded(sb *segmentBuffer, bizTag string) {
	if sb.next != nil || sb.loading {
		return // 已有预加载 / 加载中
	}
	if float64(sb.cur.remain()) >= float64(m.cfg.Step)*m.cfg.LowWater {
		return // 未到水位
	}
	// 快照回源参数（goroutine 内 load 不持锁，避免阻塞发号）
	prevStep := sb.curStep
	since := sinceLastLoad(sb)
	sb.loading = true
	go func() {
		defer func() {
			sb.mu.Lock()
			sb.loading = false
			sb.mu.Unlock()
		}()
		nb, nstep, err := m.load(context.Background(), bizTag, prevStep, since, 1)
		if err != nil {
			// 预加载失败不打断发号：cur 未耗尽前无感知；耗尽后 nextN 会同步重试
			return
		}
		sb.mu.Lock()
		defer sb.mu.Unlock()
		sb.curStep = nstep
		sb.lastLoadAt = time.Now()
		// 防御：期间若 nextN 已同步加载过更新的号段（DB 慢 + 恰好耗尽），
		// 丢弃本次较旧结果（号段浪费可接受，防 cur/next 逆序）
		if sb.next == nil && (sb.cur == nil || nb.start > sb.cur.end) {
			sb.next = nb
		}
	}()
}

// load 从 MySQL 取一个新号段：UPDATE 推进 + 回读，行锁保证多实例互斥。
// step 动态调整（对齐 Leaf）：首次回源取 t_leaf_alloc.step 为基准；
// 后续按消耗间隔调整（prevStep 传入、新值随号段一并返回，由调用方在 sb.mu 下落账），
// 并写回 t_leaf_alloc.step 供其他实例与 DBA 观察。
// minCount 为本次请求所需最小号段数（同步取号传 count；预加载传 1）：
// 一次性大批量时号段按需放大（只影响本次分配，不写回表），防 nextN 死循环。
func (m *Manager) load(ctx context.Context, bizTag string, prevStep int64, sinceLast time.Duration, minCount int) (*buffer, int64, error) {
	var alloc Alloc
	if err := m.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("biz_tag = ?", bizTag).
		First(&alloc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, fmt.Errorf("biz_tag 未注册: %s", bizTag)
		}
		return nil, 0, err
	}

	var step int64
	if prevStep <= 0 {
		// 首次回源：以表内 step 为基准（表内也无效则退回配置 step）
		step = int64(alloc.Step)
		if step <= 0 {
			step = int64(m.cfg.Step)
		}
	} else {
		step = adjustStep(prevStep, sinceLast)
	}
	size := step
	if int64(minCount) > size {
		size = int64(minCount)
	}

	// 事务：推进 max_id（并写回动态 step）+ 回读，号段 = (newMax-size, newMax]
	var newMax int64
	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Alloc{}).
			Where("biz_tag = ?", bizTag).
			Updates(map[string]any{
				"max_id": gorm.Expr("max_id + ?", size),
				"step":   step,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&Alloc{}).
			Where("biz_tag = ?", bizTag).
			Pluck("max_id", &newMax).Error
	}); err != nil {
		return nil, 0, err
	}

	return &buffer{start: newMax - size, end: newMax, pos: newMax - size}, step, nil
}

// adjustStep 动态步长：两次回源间隔即号段消耗时长，
// 消耗快于 15min → 翻倍（防高峰频繁回源），慢于 30min → 减半（省号），
// 夹在 [100, 50000]（对齐 Leaf 默认）。
func adjustStep(cur int64, interval time.Duration) int64 {
	switch {
	case interval < stepGrowBelow && cur < stepMax:
		return min(cur*2, stepMax)
	case interval > stepShrinkAbove && cur > stepMin:
		return max(cur/2, stepMin)
	default:
		return cur
	}
}

// EnsureRegistered 确保 biz_tag 在 t_leaf_alloc 有行（devops-id migrate 使用）。
// 已存在则跳过（幂等）。
func (m *Manager) EnsureRegistered(bizTags []string, description string) error {
	return m.db.Transaction(func(tx *gorm.DB) error {
		for _, tag := range bizTags {
			// INSERT IGNORE 语义：存在则跳过
			if err := tx.Exec(
				"INSERT IGNORE INTO t_leaf_alloc (biz_tag, max_id, step, description) VALUES (?, 1, ?, ?)",
				tag, m.cfg.Step, description,
			).Error; err != nil {
				return fmt.Errorf("注册 biz_tag %s 失败: %w", tag, err)
			}
		}
		return nil
	})
}

// AutoMigrate 建表（devops-id migrate 使用）。
func (m *Manager) AutoMigrate() error {
	return m.db.AutoMigrate(&Alloc{})
}
