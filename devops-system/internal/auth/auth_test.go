package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	securityjwt "github.com/stanley760/devOps/devops-infra/security/jwt"
	securitysession "github.com/stanley760/devOps/devops-infra/security/session"

	"github.com/stanley760/devOps/devops-system/internal/config"
)

// testManager 构建 miniredis + RS256 共享实现的门面。
func testManager(t *testing.T) (*Manager, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cfg := config.JWTConfig{
		SigningKey:    string(pemKey),
		KeyID:         "test-key",
		Issuer:        "devops",
		AccessExpire:  time.Hour,
		RefreshExpire: 24 * time.Hour,
	}
	pub, err := securityjwt.PublicKeyFromPrivatePEM(pemKey)
	if err != nil {
		t.Fatal(err)
	}
	keySet, err := securityjwt.NewStaticKeySet(map[string]*rsa.PublicKey{cfg.EffectiveKeyID(): pub})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := securityjwt.NewManager(cfg.SharedJWTConfig(keySet))
	if err != nil {
		t.Fatal(err)
	}
	store := securitysession.NewRedisClientStore(client, cfg.RefreshExpire)
	return New(cfg, tokens, store), mr
}

func TestIssueParseSessionRoundTrip(t *testing.T) {
	ctx := context.Background()
	m, mr := testManager(t)

	access, refresh, expiresIn, err := m.Issue(ctx, 42, "alice", 7, "tenant-7")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if access == "" || refresh == "" || expiresIn != int64(time.Hour.Seconds()) {
		t.Fatalf("issue = %q %q %d", access, refresh, expiresIn)
	}

	// 令牌为 RS256 + kid + 现网 claim 名（手工解码断言，系统模块不直接依赖 jwt 库）
	parts := strings.Split(access, ".")
	if len(parts) != 3 {
		t.Fatalf("token segments = %d", len(parts))
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "RS256" || header["kid"] != "test-key" || header["typ"] != "JWT" {
		t.Fatalf("unexpected header: %v", header)
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]any{"uid": float64(42), "username": "alice", "tid": float64(7), "tcode": "tenant-7"} {
		if payload[name] != want {
			t.Fatalf("claim %s = %v, want %v", name, payload[name], want)
		}
	}
	if _, ok := payload["sid"]; !ok {
		t.Fatal("claim sid missing")
	}

	claims, err := m.Parse(access)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" || claims.TenantID != 7 || claims.TenantCode != "tenant-7" {
		t.Fatalf("claims mismatch: %+v", claims)
	}

	// 会话键形与旧实现一致：t{tid}:auth:refresh:{sid} / sids / sid2uid
	sid := claims.SID
	if !mr.Exists("t7:auth:refresh:"+sid) || !mr.Exists("auth:sid2uid:"+sid) {
		t.Fatal("session keys missing")
	}
	if members, _ := mr.SMembers("t7:auth:sids:42"); len(members) != 1 || members[0] != sid {
		t.Fatalf("sids set = %v", members)
	}
	if !m.SessionActive(ctx, claims) {
		t.Fatal("session should be active")
	}

	tid, uid, ok := m.LookupSID(ctx, sid)
	if !ok || tid != 7 || uid != 42 {
		t.Fatalf("lookup = %d %d %v", tid, uid, ok)
	}
	if !m.SIDMatch(ctx, 7, sid, 42) || m.SIDMatch(ctx, 7, sid, 99) {
		t.Fatal("sid match mismatch")
	}
}

func TestRotateCreatesNewBeforeRevokingOld(t *testing.T) {
	ctx := context.Background()
	m, mr := testManager(t)

	_, oldSID, _, err := m.Issue(ctx, 42, "alice", 7, "tenant-7")
	if err != nil {
		t.Fatal(err)
	}
	access, newSID, _, err := m.Rotate(ctx, oldSID, 42, "alice", 7, "tenant-7")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newSID == oldSID {
		t.Fatal("sid not rotated")
	}
	if mr.Exists("t7:auth:refresh:"+oldSID) || mr.Exists("auth:sid2uid:"+oldSID) {
		t.Fatal("old session still present")
	}
	if !mr.Exists("t7:auth:refresh:" + newSID) {
		t.Fatal("new session missing")
	}
	if members, _ := mr.SMembers("t7:auth:sids:42"); len(members) != 1 || members[0] != newSID {
		t.Fatalf("sids set after rotate = %v", members)
	}
	claims, err := m.Parse(access)
	if err != nil || claims.SID != newSID {
		t.Fatalf("rotated token parse = %+v, %v", claims, err)
	}
}

func TestRevokeAllAndTenantSessions(t *testing.T) {
	ctx := context.Background()
	m, mr := testManager(t)

	acc1, _, _, _ := m.Issue(ctx, 11, "u1", 7, "tenant-7")
	acc2, _, _, _ := m.Issue(ctx, 22, "u2", 7, "tenant-7")
	_, _, _, _ = m.Issue(ctx, 33, "u3", 9, "tenant-9")
	if mr.Exists("auth:sid2uid:x") {
		t.Fatal("unexpected key")
	}

	c1, _ := m.Parse(acc1)
	c2, _ := m.Parse(acc2)

	m.Logout(ctx, c1)
	if mr.Exists("t7:auth:refresh:" + c1.SID) {
		t.Fatal("logout did not revoke current session")
	}

	m.RevokeAllSessions(ctx, 7, 22)
	if mr.Exists("t7:auth:refresh:" + c2.SID) {
		t.Fatal("revoke-all did not remove user session")
	}

	m.RevokeTenantSessions(ctx, 9)
	for _, key := range mr.Keys() {
		if strings.HasPrefix(key, "t9:auth:refresh:") || strings.HasPrefix(key, "auth:sid2uid:") {
			t.Fatalf("tenant revoke left key: %s", key)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	m, _ := testManager(t)
	if _, err := m.Parse("not-a-token"); err == nil {
		t.Fatal("expected parse failure")
	}
}

// TestParseRejectsLegacyHS256 迁移决策回归：存量 HS256 token（同 issuer、未过期、
// 旧 claims 形制）在 RS256 升级后整体失效——共享解析只接受 RS256。
func TestParseRejectsLegacyHS256(t *testing.T) {
	m, _ := testManager(t)

	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := b64([]byte(`{"uid":42,"username":"alice","sid":"legacy","tid":7,"tcode":"tenant-7","iss":"devops","exp":` +
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}`))
	mac := hmac.New(sha256.New, []byte("legacy-hs256-secret"))
	mac.Write([]byte(header + "." + payload))
	legacy := header + "." + payload + "." + b64(mac.Sum(nil))

	if _, err := m.Parse(legacy); err == nil {
		t.Fatal("legacy HS256 token must be rejected after RS256 rollout")
	}
}
