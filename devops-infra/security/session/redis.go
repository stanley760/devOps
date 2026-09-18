package session

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	cacheRedis "devops-infra/cache/redis"
	goredis "github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *goredis.Client
	ttl    time.Duration
}

func NewRedisStore(client *cacheRedis.Client, ttl time.Duration) *RedisStore {
	if client == nil {
		return &RedisStore{ttl: ttl}
	}
	return &RedisStore{client: client.Redis(), ttl: ttl}
}

func NewRedisClientStore(client *goredis.Client, ttl time.Duration) *RedisStore {
	return &RedisStore{client: client, ttl: ttl}
}

func (s *RedisStore) Create(ctx context.Context, session Session) error {
	if err := validateSession(session); err != nil {
		return err
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("session: redis client is nil")
	}
	pipe := s.client.Pipeline()
	pipe.Set(ctx, refreshKey(session.TenantID, session.SessionID), session.UserID, session.TTLOr(s.ttl))
	pipe.SAdd(ctx, sidsKey(session.TenantID, session.UserID), session.SessionID)
	pipe.Set(ctx, sid2uidKey(session.SessionID),
		fmt.Sprintf("%d:%d", session.TenantID, session.UserID), session.TTLOr(s.ttl))
	_, err := pipe.Exec(ctx)
	return err
}

func (s Session) TTLOr(defaultTTL time.Duration) time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return defaultTTL
}

func (s *RedisStore) Active(ctx context.Context, ref SessionRef) (bool, error) {
	if err := validateRef(ref); err != nil {
		return false, err
	}
	if s == nil || s.client == nil {
		return false, fmt.Errorf("session: redis client is nil")
	}
	n, err := s.client.Exists(ctx, refreshKey(ref.TenantID, ref.SessionID)).Result()
	return n > 0, err
}

func (s *RedisStore) Lookup(ctx context.Context, sessionID string) (int64, int64, bool) {
	if sessionID == "" || s == nil || s.client == nil {
		return 0, 0, false
	}
	value, err := s.client.Get(ctx, sid2uidKey(sessionID)).Result()
	if err != nil {
		return 0, 0, false
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	tenantID, err1 := strconv.ParseInt(parts[0], 10, 64)
	userID, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || tenantID <= 0 || userID <= 0 {
		return 0, 0, false
	}
	return tenantID, userID, true
}

func (s *RedisStore) Matches(ctx context.Context, ref SessionRef, userID int64) (bool, error) {
	if err := validateRef(ref); err != nil {
		return false, err
	}
	if userID <= 0 {
		return false, ErrInvalidUserID
	}
	if s == nil || s.client == nil {
		return false, fmt.Errorf("session: redis client is nil")
	}
	value, err := s.client.Get(ctx, refreshKey(ref.TenantID, ref.SessionID)).Result()
	if err != nil {
		if err == goredis.Nil {
			return false, nil
		}
		return false, err
	}
	return value == strconv.FormatInt(userID, 10), nil
}

func (s *RedisStore) Revoke(ctx context.Context, ref SessionRef) error {
	if err := validateRef(ref); err != nil {
		return err
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("session: redis client is nil")
	}
	pipe := s.client.Pipeline()
	pipe.Del(ctx, refreshKey(ref.TenantID, ref.SessionID), sid2uidKey(ref.SessionID))
	pipe.SRem(ctx, sidsKey(ref.TenantID, ref.UserID), ref.SessionID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) RevokeUser(ctx context.Context, tenantID, userID int64) error {
	if tenantID <= 0 {
		return ErrInvalidTenantID
	}
	if userID <= 0 {
		return ErrInvalidUserID
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("session: redis client is nil")
	}
	key := sidsKey(tenantID, userID)
	sids, err := s.client.SMembers(ctx, key).Result()
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	for _, sid := range sids {
		pipe.Del(ctx, refreshKey(tenantID, sid), sid2uidKey(sid))
	}
	pipe.Del(ctx, key)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) RevokeTenant(ctx context.Context, tenantID int64) error {
	if tenantID <= 0 {
		return ErrInvalidTenantID
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("session: redis client is nil")
	}
	pattern := fmt.Sprintf("t%d:auth:sids:*", tenantID)
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			sids, err := s.client.SMembers(ctx, key).Result()
			if err != nil {
				return err
			}
			pipe := s.client.Pipeline()
			for _, sid := range sids {
				pipe.Del(ctx, refreshKey(tenantID, sid), sid2uidKey(sid))
			}
			pipe.Del(ctx, key)
			if _, err := pipe.Exec(ctx); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
