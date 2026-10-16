package oauth2

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// RedisStateStore state 的 Redis 实现。
//
// 键形：t1:auth:oauth2state:{state} = JSON(StateValue)，TTL = stateTTL。
// 一次性由 Redis GETDEL 保证（Redis 6.2+）；低版本回退到 Lua 脚本，
// 避免「GET 校验通过但 DEL 失败」导致同一 state 被用两次。
type RedisStateStore struct {
	rdb      *goredis.Client
	prefix   string
	stateTTL time.Duration
}

// NewRedisStateStore 创建 store。stateTTL 建议 5-10 分钟：
// 超过 10 分钟的授权请求基本是用户放着不管，再放开反而增大重放窗口。
func NewRedisStateStore(rdb *goredis.Client, prefix string, stateTTL time.Duration) *RedisStateStore {
	if stateTTL <= 0 {
		stateTTL = 10 * time.Minute
	}
	return &RedisStateStore{rdb: rdb, prefix: prefix, stateTTL: stateTTL}
}

func (s *RedisStateStore) key(state string) string {
	return s.prefix + ":auth:oauth2state:" + state
}

// Save 存入 state。
func (s *RedisStateStore) Save(ctx context.Context, state string, val StateValue, ttl time.Duration) error {
	if s == nil || s.rdb == nil {
		return errNoRedis
	}
	if ttl <= 0 {
		ttl = s.stateTTL
	}
	payload, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, s.key(state), payload, ttl).Err()
}

// Verify 校验并一次性取出 state。
//
// 用 Lua 保证「读到即删」的原子性：若先 GET 再 DEL，两个并发请求可能都读到
// 同一个 state 并各自完成登录——state 一次性失效形同虚设。
func (s *RedisStateStore) Verify(ctx context.Context, state string) (StateValue, error) {
	var empty StateValue
	if s == nil || s.rdb == nil {
		return empty, errNoRedis
	}
	if state == "" {
		return empty, ErrStateInvalid
	}
	const getdelScript = `
local v = redis.call('GET', KEYS[1])
if v == false then return nil end
redis.call('DEL', KEYS[1])
return v`

	raw, err := s.rdb.Eval(ctx, getdelScript, []string{s.key(state)}).Result()
	if err == goredis.Nil {
		return empty, ErrStateInvalid
	}
	if err != nil {
		return empty, err
	}
	payload, ok := raw.(string)
	if !ok || payload == "" {
		return empty, ErrStateInvalid
	}
	var val StateValue
	if err := json.Unmarshal([]byte(payload), &val); err != nil {
		return empty, ErrStateInvalid
	}
	return val, nil
}

// errNoRedis 未装配 Redis。
var errNoRedis = errNoRedisType("oauth2: redis client is nil")

type errNoRedisType string

func (e errNoRedisType) Error() string { return string(e) }
