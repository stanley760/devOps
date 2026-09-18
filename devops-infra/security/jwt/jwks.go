package jwt

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
)

// JWKSDocument 生成 RFC 7517 JWKS 文档（{"keys":[...]}），
// 供 Higress jwt-auth 插件（jwks 或 jwksFromUrl）与各服务做 RS256 验签。
// 仅输出公钥材料（n/e），不暴露私钥。
func JWKSDocument(pub *rsa.PublicKey, keyID string) (map[string]any, error) {
	if pub == nil || pub.N == nil || pub.E <= 0 {
		return nil, fmt.Errorf("%w: missing public key", ErrInvalidConfig)
	}
	if keyID == "" {
		return nil, fmt.Errorf("%w: missing key id", ErrInvalidConfig)
	}
	key := map[string]any{
		"kty": "RSA",
		"kid": keyID,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
	return map[string]any{"keys": []any{key}}, nil
}

// JWKS 返回管理器签名密钥对应的 JWKS 文档。
func (m *Manager) JWKS() (map[string]any, error) {
	if m == nil || m.privateKey == nil {
		return nil, ErrInvalidConfig
	}
	doc, err := JWKSDocument(&m.privateKey.PublicKey, m.config.KeyID)
	if err != nil {
		return nil, err
	}
	// 确认可序列化，调用方直接交给 HTTP 层。
	if _, err := json.Marshal(doc); err != nil {
		return nil, err
	}
	return doc, nil
}
