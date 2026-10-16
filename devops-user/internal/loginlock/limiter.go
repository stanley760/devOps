// Package loginlock 提供 C 端登录失败的限流与账号临时锁定（基于 Redis）。
//
// ## 为什么需要
//
// bcrypt 只能拖慢单次爆破（cost=10 约数十毫秒），但无法阻止高速尝试。
// C 端开放注册、用户名可枚举，撞库与暴力破解风险高于内部后台，必须叠加：
//
//  1. **IP 维度限流**：拦住「一个 IP 批量试不同用户名」；
//  2. **账号维度锁定**：拦住「多个 IP 集中试同一个账号」。
//
// 两者缺一不可：只有 IP 限流会被代理池绕过，只有账号锁定会误伤
// 「攻击者用一个 IP 扫全站用户名」（该账号不会被锁，但 IP 应被拦）。
//
// ## 计数与解锁
//
// 滑动窗口用固定窗口计数（Redis INCR + EXPIRE）：实现简单、开销小，
// 代价是窗口边界可能有 2倍瞬时量。对登录这种非高频场景可接受。
//
// 锁定用「失败计数达到阈值 → 锁定 TTL」的两段式：先累计失败次数，
// 达标后写锁定键并设置 TTL，期间直接拒绝；任一次登录成功即清空计数。
package loginlock

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ErrLocked 账号已被临时锁定（达到失败阈值）。
var ErrLocked = errors.New("账号已锁定，请稍后再试")

// ErrTooManyAttempts 触发 IP 维度限流。
var ErrTooManyAttempts = errors.New("尝试过于频繁，请稍后再试")

// 失败信息占位符。
const (
	// failSuffix 失败计数键后缀。
	failSuffix = "auth:loginfail"
	// lockSuffix 账号锁定键后缀。
	lockSuffix = "auth:loginlock"
)

// Config 限流与锁定参数。
type Config struct {
	// MaxIPFails 单IP 在窗口内允许的最大失败次数；0 表示不限。
	MaxIPFails int
	// IPWindow IP 计数窗口。
	IPWindow time.Duration
	// MaxUserFails 单账号在窗口内允许的最大失败次数；0 表示不锁账号。
	MaxUserFails int
	// UserWindow 账号失败计数窗口。
	UserWindow time.Duration
	// LockTTL 账号锁定时长；失败计数达标后锁定这么久。
	LockTTL time.Duration
	// Enabled 总开关；false 时全部放行（dev/测试可关）。
	Enabled bool
}

// DefaultConfig 返回一组保守默认值。
//
// 取值权衡：
//   - MaxIPFails=20 / 5min：正常人几乎碰不到，脚本批量尝试会被拦。
//   - MaxUserFails=5 / 15min：允许用户输错几次，挡住持续爆破。
//   - LockTTL=15min：够短不至于让用户永久被锁，够长让爆破无收益。
func DefaultConfig() Config {
	return Config{
		Enabled:      true,
		MaxIPFails:   20,
		IPWindow:     5 * time.Minute,
		MaxUserFails: 5,
		UserWindow:   15 * time.Minute,
		LockTTL:      15 * time.Minute,
	}
}

// Limiter 登录失败限流与账号锁定。
type Limiter struct {
	rdb    *goredis.Client
	prefix string
	cfg    Config
}

// New 创建限流器。prefix 建议传 "t1"（C 端单租户）。
func New(rdb *goredis.Client, prefix string, cfg Config) *Limiter {
	return &Limiter{rdb: rdb, prefix: prefix, cfg: cfg}
}

// failKey IP 维度失败计数键。
func (l *Limiter) failKey(clientIP string) string {
	return fmt.Sprintf("%s:%s:%s", l.prefix, failSuffix, clientIP)
}

// lockKey 账号维度锁定键。
func (l *Limiter) lockKey(username string) string {
	return fmt.Sprintf("%s:%s:%s", l.prefix, lockSuffix, username)
}

// Check 在尝试登录前调用：账号未锁定且 IP 未超限则放行。
//
// 返回 ErrLocked / ErrTooManyAttempts 表示拒绝。
func (l *Limiter) Check(ctx context.Context, clientIP, username string) error {
	if !l.cfg.Enabled || l.rdb == nil {
		return nil
	}
	// 账号锁定优先判断（更具体，给用户可执行的提示）。
	if l.cfg.MaxUserFails > 0 {
		locked, err := l.rdb.Exists(ctx, l.lockKey(username)).Result()
		if err != nil {
			// Redis 故障：fail-closed 拒绝，还是放行？
			// 选择 fail-open 并记录——登录接口不可用比放行几个尝试更糟，
			// 但生产应配 Redis 高可用；此处放行以保证可用性。
			return nil
		}
		if locked > 0 {
			return ErrLocked
		}
	}
	// IP 维度限流。
	if l.cfg.MaxIPFails > 0 {
		n, err := l.rdb.Get(ctx, l.failKey(clientIP)).Int64()
		if err != nil && !errors.Is(err, goredis.Nil) {
			return nil // 同上：fail-open
		}
		if n >= int64(l.cfg.MaxIPFails) {
			return ErrTooManyAttempts
		}
	}
	return nil
}

// Fail 记录一次登录失败；达到账号阈值时加锁。
func (l *Limiter) Fail(ctx context.Context, clientIP, username string) {
	if !l.cfg.Enabled || l.rdb == nil {
		return
	}
	if l.cfg.MaxIPFails > 0 {
		key := l.failKey(clientIP)
		pipe := l.rdb.Pipeline()
		pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, l.cfg.IPWindow)
		_, _ = pipe.Exec(ctx)
	}
	if l.cfg.MaxUserFails > 0 {
		key := l.failKey("u:" + username)
		pipe := l.rdb.Pipeline()
		pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, l.cfg.UserWindow)
		_, _ = pipe.Exec(ctx)

		n, err := l.rdb.Get(ctx, key).Int64()
		if err == nil && n >= int64(l.cfg.MaxUserFails) {
			// 达标即锁定；NX 保证已锁定时不续期，避免无限延长。
			l.rdb.SetNX(ctx, l.lockKey(username), "1", l.cfg.LockTTL)
		}
	}
}

// Success 登录成功后清空该账号的失败计数（IP 计数不清，否则正常用户
// 多次输错就永久触发 IP 限流）。
func (l *Limiter) Success(ctx context.Context, username string) {
	if !l.cfg.Enabled || l.rdb == nil {
		return
	}
	l.rdb.Del(ctx, l.failKey("u:"+username))
}

// Reset 管理员解锁（封禁处理/用户申诉后调用）。
func (l *Limiter) Reset(ctx context.Context, username string) {
	if l.rdb == nil {
		return
	}
	pipe := l.rdb.Pipeline()
	pipe.Del(ctx, l.lockKey(username))
	pipe.Del(ctx, l.failKey("u:"+username))
	_, _ = pipe.Exec(ctx)
}
