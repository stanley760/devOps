package jwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// StaticKeySet 是一个固定的 kid → 公钥 集合，适用于单签发方部署或测试。
// JWKS 轮换场景应实现 KeySet 接口，从远端 JWKS 文档刷新内部映射。
type StaticKeySet struct {
	keys map[string]*rsa.PublicKey
}

// NewStaticKeySet 构建静态公钥集合。
func NewStaticKeySet(keys map[string]*rsa.PublicKey) (*StaticKeySet, error) {
	if len(keys) == 0 {
		return nil, ErrInvalidConfig
	}
	copied := make(map[string]*rsa.PublicKey, len(keys))
	for kid, key := range keys {
		if kid == "" || key == nil {
			return nil, ErrInvalidConfig
		}
		copied[kid] = key
	}
	return &StaticKeySet{keys: copied}, nil
}

// Lookup 按 kid 解析公钥。
func (s *StaticKeySet) Lookup(kid string) (*rsa.PublicKey, error) {
	if s == nil {
		return nil, ErrInvalidConfig
	}
	key, ok := s.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: unknown kid %q", ErrInvalidToken, kid)
	}
	return key, nil
}

// PublicKeyFromPrivatePEM 从 PKCS#1/PKCS#8 私钥 PEM 解析出配套公钥，
// 供服务用自身私钥组装自验签 KeySet（单签发方部署）。
func PublicKeyFromPrivatePEM(privatePEM []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(privatePEM)
	if block == nil {
		return nil, ErrInvalidConfig
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, ErrInvalidConfig
		}
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, ErrInvalidConfig
		}
		return &key.PublicKey, nil
	}
	return &key.PublicKey, nil
}
