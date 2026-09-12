// Package auth 实现 JWT 双 token 与 Redis 会话管理。
//
//   - Access：HS256，claims 携带 user_id/username/sid/tenant_id/tenant_code，短期（2h）
//   - Refresh：UUID（sid），Redis 持会话态（键空间租户分段，方案 6.5）：
//     t{tid}:auth:refresh:{sid} = user_id，TTL = refresh_expire（7d）
//     t{tid}:auth:sids:{uid}    = SET<sid>（RevokeAllSessions 用）
//     auth:sid2uid:{sid}        = "tid:uid"（refresh 端点由 sid 定位会话归属）
//   - 每个请求校验 EXISTS refresh:{sid} —— 踢下线（logout/改密/停用）后旧 access 立即 401
//   - refresh 端点轮换 sid（旧删新建），对齐 refresh token rotation 最佳实践
package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"devops-infra/errs"

	"devops-system/internal/config"
	"devops-system/internal/store"
)

// AccessClaims access token 载荷。
type AccessClaims struct {
	UserID     int64  `json:"uid"`
	Username   string `json:"username"`
	SID        string `json:"sid"`
	TenantID   int64  `json:"tid"`
	TenantCode string `json:"tcode"`
	jwt.RegisteredClaims
}

// Manager JWT + 会话管理器。
type Manager struct {
	cfg config.JWTConfig
	rdb *redis.Client
}

// New 创建管理器。
func New(cfg config.JWTConfig) *Manager {
	return &Manager{cfg: cfg, rdb: store.Get()}
}

// ---- 键空间（方案 6.5） ----

func refreshKey(tenantID int64, sid string) string {
	return fmt.Sprintf("t%d:auth:refresh:%s", tenantID, sid)
}

func sidsKey(tenantID, userID int64) string {
	return fmt.Sprintf("t%d:auth:sids:%d", tenantID, userID)
}

func sid2uidKey(sid string) string {
	return "auth:sid2uid:" + sid
}

// Issue 双 token 签发（登录 / refresh 轮换共用）。
// 返回 access token、refresh token（即 sid）与 access 有效期秒数。
func (m *Manager) Issue(ctx context.Context, userID int64, username string, tenantID int64, tenantCode string) (access, refresh string, expiresIn int64, err error) {
	sid := uuid.NewString()

	now := time.Now()
	claims := &AccessClaims{
		UserID:     userID,
		Username:   username,
		SID:        sid,
		TenantID:   tenantID,
		TenantCode: tenantCode,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.cfg.Issuer,
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(now.Add(m.cfg.AccessExpire)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        sid,
		},
	}
	access, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(m.cfg.SigningKey))
	if err != nil {
		return "", "", 0, errs.Wrap(err, errs.CodeInternal, "签发 token 失败")
	}

	pipe := m.rdb.Pipeline()
	pipe.Set(ctx, refreshKey(tenantID, sid), userID, m.cfg.RefreshExpire)
	pipe.SAdd(ctx, sidsKey(tenantID, userID), sid)
	pipe.Set(ctx, sid2uidKey(sid), fmt.Sprintf("%d:%d", tenantID, userID), m.cfg.RefreshExpire)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", "", 0, errs.Wrap(err, errs.CodeInternal, "写入会话失败")
	}
	return access, sid, int64(m.cfg.AccessExpire.Seconds()), nil
}

// Parse 校验并解析 access token（不查 Redis，会话态由中间件另行校验）。
func (m *Manager) Parse(token string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非法签名算法: %v", t.Header["alg"])
		}
		return []byte(m.cfg.SigningKey), nil
	}, jwt.WithIssuer(m.cfg.Issuer))
	if err != nil {
		return nil, errs.New(errs.CodeUnauthorized, "未登录或会话已过期")
	}
	return claims, nil
}

// SessionActive 会话是否仍有效（logout/踢下线/改密后立即失效的关键）。
func (m *Manager) SessionActive(ctx context.Context, claims *AccessClaims) bool {
	return m.rdb.Exists(ctx, refreshKey(claims.TenantID, claims.SID)).Val() > 0
}

// LookupSID 由 refresh token（sid）定位会话归属（refresh 端点用）。
func (m *Manager) LookupSID(ctx context.Context, sid string) (tenantID, userID int64, ok bool) {
	val, err := m.rdb.Get(ctx, sid2uidKey(sid)).Result()
	if err != nil {
		return 0, 0, false
	}
	parts := strings.SplitN(val, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	tid, err1 := strconv.ParseInt(parts[0], 10, 64)
	uid, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return tid, uid, true
}

// SIDMatch 校验 sid 会话确属该用户。
func (m *Manager) SIDMatch(ctx context.Context, tenantID int64, sid string, userID int64) bool {
	val, err := m.rdb.Get(ctx, refreshKey(tenantID, sid)).Result()
	return err == nil && val == strconv.FormatInt(userID, 10)
}

// Rotate 轮换会话：签发新双 token 并撤销旧 sid（service.Refresh 校验通过后调用）。
func (m *Manager) Rotate(ctx context.Context, oldSID string, userID int64, username string, tenantID int64, tenantCode string) (access, refresh string, expiresIn int64, err error) {
	access, refresh, expiresIn, err = m.Issue(ctx, userID, username, tenantID, tenantCode)
	if err != nil {
		return "", "", 0, err
	}
	// 轮换：删旧会话与索引，sids 集合同步收紧
	pipe := m.rdb.Pipeline()
	pipe.Del(ctx, refreshKey(tenantID, oldSID))
	pipe.Del(ctx, sid2uidKey(oldSID))
	pipe.SRem(ctx, sidsKey(tenantID, userID), oldSID)
	pipe.Exec(ctx)
	return access, refresh, expiresIn, nil
}

// Logout 撤销当前会话。
func (m *Manager) Logout(ctx context.Context, claims *AccessClaims) {
	pipe := m.rdb.Pipeline()
	pipe.Del(ctx, refreshKey(claims.TenantID, claims.SID))
	pipe.Del(ctx, sid2uidKey(claims.SID))
	pipe.SRem(ctx, sidsKey(claims.TenantID, claims.UserID), claims.SID)
	pipe.Exec(ctx)
}

// RevokeAllSessions 撤销用户全部会话（改密/重置密码/停用/删除用户、停用租户）。
func (m *Manager) RevokeAllSessions(ctx context.Context, tenantID, userID int64) {
	skey := sidsKey(tenantID, userID)
	sids, err := m.rdb.SMembers(ctx, skey).Result()
	if err != nil {
		return
	}
	pipe := m.rdb.Pipeline()
	for _, sid := range sids {
		pipe.Del(ctx, refreshKey(tenantID, sid))
		pipe.Del(ctx, sid2uidKey(sid))
	}
	pipe.Del(ctx, skey)
	pipe.Exec(ctx)
}

// RevokeTenantSessions 撤销租户全部会话（停用租户 → 全员下线）。
// 遍历键空间 t{tid}:auth:sids:*，逐用户撤销并清理 sid2uid 索引。
func (m *Manager) RevokeTenantSessions(ctx context.Context, tenantID int64) {
	pattern := fmt.Sprintf("t%d:auth:sids:*", tenantID)
	var cursor uint64
	for {
		keys, next, err := m.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return
		}
		pipe := m.rdb.Pipeline()
		for _, skey := range keys {
			sids, err := m.rdb.SMembers(ctx, skey).Result()
			if err != nil {
				continue
			}
			for _, sid := range sids {
				pipe.Del(ctx, refreshKey(tenantID, sid))
				pipe.Del(ctx, sid2uidKey(sid))
			}
			pipe.Del(ctx, skey)
		}
		pipe.Exec(ctx)
		if cursor = next; cursor == 0 {
			return
		}
	}
}
