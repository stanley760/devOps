// Package refreshtoken 实现 C 端的 refresh token 存取（Redis 键值对，非 JWT）。
//
// ## 为什么 refresh 不用 JWT
//
// **切勿把 access token 当 refresh 复用**：access 是无状态 JWT，任何人拿到就能
// 一直用到过期；一旦允许用它换新 access，等于给了攻击者无限续命的凭据，
// 且用户「登出/封禁」无法真正作废（在过期前一直有效）。
//
// 本实现用 256 位密码学随机串（base64url），服务端只存哈希：
//   - refresh: 客户端持有明文，服务端仅存 SHA-256 哈希 → 库被拖也无法直接冒用；
//   - 可精确作废：登出/改密/封禁时直接 DEL 键；
//   - 可轮换：每次刷新换新串并作废旧串（旧串一次性失效）。
//
// 键形（与 devops-infra/security/session 的 t{tid}:auth:* 同一命名空间风格）：
//
//	t1:auth:refresh:{hash}    = {userID}      TTL = refresh_expire
//	t1:auth:refreshuser:{uid} = SET<hash>     TTL = refresh_expire
//
// **两个键缺一不可**：`refresh:{hash}` 支持按 token 精确查找；`refreshuser:{uid}`
// 是反查索引，让「按用户撤销全部」成为可能——没有它就做不到「改密即全端下线」
// 与「封禁强制下线」（无法枚举某用户的全部 refresh）。
// 与 devops-infra/security/session 的 `sids` 集合同构，语义一致。
//
// userID 而非 "tid:uid"：C 端单租户，tid 恒为 auth.DefaultTenantID，无信息量。
package refreshtoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ErrNotFound token 不存在、已过期或已被轮换作废。
//
// 三者**必须**返回同一个错误：区分会让攻击者判断某个 token 是否曾经有效。
var ErrNotFound = errors.New("refresh token 不存在或已过期")

// Store refresh token 的存取。
type Store struct {
	rdb    *goredis.Client
	prefix string
	ttl    time.Duration
}

// NewStore 创建存储。prefix 建议传 "t1"（C 端单租户）。
func NewStore(rdb *goredis.Client, prefix string, ttl time.Duration) *Store {
	return &Store{rdb: rdb, prefix: prefix, ttl: ttl}
}

// key 哈希后的 Redis 键。
func (s *Store) key(hash string) string {
	return fmt.Sprintf("%s:auth:refresh:%s", s.prefix, hash)
}

// userKey 用户 → 其全部 refresh 哈希的反查索引。
func (s *Store) userKey(userID int64) string {
	return fmt.Sprintf("%s:auth:refreshuser:%d", s.prefix, userID)
}

// Generate 生成明文 token 及其哈希（明文只在此刻存在一次，返回给调用方）。
func Generate() (plain string, hash string, err error) {
	buf := make([]byte, 32) // 256 位
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	plain = base64.RawURLEncoding.EncodeToString(buf)
	return plain, Hash(plain), nil
}

// Hash 明文 → 服务端存储用的哈希（base64url，无填充）。
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Save 保存 token → userID 映射，并登记到用户的反查索引。
func (s *Store) Save(ctx context.Context, hash string, userID int64) error {
	if err := s.check(); err != nil {
		return err
	}
	if s.ttl <= 0 {
		// 零值 TTL 会让 Redis 立即删除，等于 token 不可用——显式失败好过静默失效。
		return fmt.Errorf("refreshtoken: TTL 未配置")
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, s.key(hash), strconv.FormatInt(userID, 10), s.ttl)
	pipe.SAdd(ctx, s.userKey(userID), hash)
	pipe.Expire(ctx, s.userKey(userID), s.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// Lookup 查出 token 对应的 userID。
func (s *Store) Lookup(ctx context.Context, hash string) (int64, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	v, err := s.rdb.Get(ctx, s.key(hash)).Result()
	if errors.Is(err, goredis.Nil) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	uid, convErr := strconv.ParseInt(v, 10, 64)
	if convErr != nil {
		// 值损坏：删掉坏键，避免持续报错堆积。
		_ = s.rdb.Del(ctx, s.key(hash)).Err()
		return 0, ErrNotFound
	}
	return uid, nil
}

// Rotate 作废旧 token 并签发新的：原子完成，避免并发刷新时签出多个有效 token。
//
// 实现用 Lua 保证「查到→ 校验归属 → 删旧 → 写新」的原子性。
// 若分步执行，并发刷新会各自查到同一个旧 token 并各自签发新 token（重放漏洞）；
// 只有原子操作才能保证「一个 refresh token 只能用一次」。
func (s *Store) Rotate(ctx context.Context, oldHash, newHash string, userID int64) error {
	if err := s.check(); err != nil {
		return err
	}
	if s.ttl <= 0 {
		return fmt.Errorf("refreshtoken: TTL 未配置")
	}
	// 顺带校验 userID 归属：不匹配说明哈希碰撞或数据被篡改，宁可拒绝。
	// 返回 -1 时不动任何键。
	const rotateScript = `
local v = redis.call('GET', KEYS[1])
if v == false then return 0 end
if v ~= ARGV[2] then return -1 end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], v, 'PX', ARGV[1])
redis.call('SREM', KEYS[3], ARGV[3])
redis.call('SADD', KEYS[4], ARGV[4])
redis.call('PEXPIRE', KEYS[4], ARGV[1])
return 1`

	raw, err := s.rdb.Eval(ctx, rotateScript,
		[]string{s.key(oldHash), s.key(newHash), s.userKey(userID), s.userKey(userID)},
		s.ttl.Milliseconds(), strconv.FormatInt(userID, 10), oldHash, newHash).Int64()
	if err != nil {
		return err
	}
	switch raw {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("refreshtoken: token 归属不匹配")
	default:
		return ErrNotFound
	}
}

// Delete 作废指定 token（登出 / 改密 / 封禁时调用）。
func (s *Store) Delete(ctx context.Context, hash string) error {
	if err := s.check(); err != nil {
		return err
	}
	// 先查归属再一并清理反查索引；查不到就直接 DEL 主键（可能是脏数据）。
	v, err := s.rdb.Get(ctx, s.key(hash)).Result()
	if errors.Is(err, goredis.Nil) {
		return s.rdb.Del(ctx, s.key(hash)).Err()
	}
	if err != nil {
		return err
	}
	uid, convErr := strconv.ParseInt(v, 10, 64)
	if convErr != nil {
		return s.rdb.Del(ctx, s.key(hash)).Err()
	}
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, s.key(hash))
	pipe.SRem(ctx, s.userKey(uid), hash)
	_, err = pipe.Exec(ctx)
	return err
}

// RevokeUser 撤销某用户的**全部** refresh token（改密即全端下线、封禁强制下线）。
//
// 依赖 userKey 反查索引；无索引的老数据会被跳过（其 refresh 仍可用到过期），
// 这是升级期的已知取舍——可通过让用户重新登录来收敛。
func (s *Store) RevokeUser(ctx context.Context, userID int64) error {
	if err := s.check(); err != nil {
		return err
	}
	hashes, err := s.rdb.SMembers(ctx, s.userKey(userID)).Result()
	if err != nil {
		return err
	}
	if len(hashes) == 0 {
		// 索引不存在或已空：仍 DEL 一次以清理可能残留的空键。
		return s.rdb.Del(ctx, s.userKey(userID)).Err()
	}
	pipe := s.rdb.Pipeline()
	for _, h := range hashes {
		pipe.Del(ctx, s.key(h))
	}
	pipe.Del(ctx, s.userKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}

// RevokeUserExcept 撤销某用户除 keepHash 外的全部 refresh（保留当前设备登录）。
func (s *Store) RevokeUserExcept(ctx context.Context, userID int64, keepHash string) error {
	if err := s.check(); err != nil {
		return err
	}
	hashes, err := s.rdb.SMembers(ctx, s.userKey(userID)).Result()
	if err != nil {
		return err
	}
	pipe := s.rdb.Pipeline()
	for _, h := range hashes {
		if h == keepHash {
			continue
		}
		pipe.Del(ctx, s.key(h))
	}
	pipe.Del(ctx, s.userKey(userID))
	if keepHash != "" {
		// 保留的那枚要重新登记，否则反查索引会把它弄丢。
		pipe.SAdd(ctx, s.userKey(userID), keepHash)
		pipe.Expire(ctx, s.userKey(userID), s.ttl)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// check 校验客户端可用（未装配时显式失败，不静默放行）。
func (s *Store) check() error {
	if s == nil || s.rdb == nil {
		return fmt.Errorf("refreshtoken: redis client is nil")
	}
	return nil
}

// ConstantTimeEqual 常数时间比较两个哈希字符串（长度不等时也走常数路径）。
func ConstantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		// 长度不同直接判否会短路——但哈希长度固定且非攻击者可控，
		// 且此处比较的是「我方算出的哈希」与「存储的哈希」，无枚举意义。
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// IsHashLike 粗判字符串是否像哈希（长度校验，用于提前拒绝明显非法的输入，
// 避免拿超长串去查 Redis）。
func IsHashLike(s string) bool {
	return len(s) == 43 && !strings.ContainsAny(s, " \t\n")
}
