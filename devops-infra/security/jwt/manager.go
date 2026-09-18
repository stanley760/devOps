package jwt

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strconv"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// Manager issues RS256 access tokens and verifies them using a KeySet.
type Manager struct {
	config     Config
	privateKey *rsa.PrivateKey
}

func NewManager(config Config) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	block, _ := pem.Decode(config.PrivateKeyPEM)
	if block == nil {
		return nil, ErrInvalidConfig
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		if parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
			var ok bool
			key, ok = parsed.(*rsa.PrivateKey)
			if !ok {
				return nil, ErrInvalidConfig
			}
		} else {
			return nil, ErrInvalidConfig
		}
	}
	return &Manager{config: config, privateKey: key}, nil
}

func (m *Manager) Issue(ctx context.Context, subject Subject, sessionID string) (TokenPair, error) {
	if m == nil || m.privateKey == nil {
		return TokenPair{}, ErrInvalidConfig
	}
	_ = ctx
	now := time.Now()
	claims := Claims{
		UID: subject.UserID, Username: subject.Username, SessionID: sessionID,
		TenantID: subject.TenantID, TenantCode: subject.TenantCode,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer: m.config.Issuer, Subject: strconv.FormatInt(subject.UserID, 10),
			IssuedAt: jwtlib.NewNumericDate(now), NotBefore: jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(m.config.AccessExpire)),
		},
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, claims)
	token.Header["kid"] = m.config.KeyID
	signed, err := token.SignedString(m.privateKey)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: signed, SessionID: sessionID,
		AccessExpiresIn: int64(m.config.AccessExpire.Seconds())}, nil
}

func (m *Manager) ParseAccess(tokenString string) (*Claims, error) {
	if m == nil || m.config.KeySet == nil {
		return nil, ErrInvalidConfig
	}
	if tokenString == "" {
		return nil, ErrInvalidToken
	}
	claims := new(Claims)
	parsed, err := jwtlib.ParseWithClaims(tokenString, claims, func(token *jwtlib.Token) (any, error) {
		if token.Method != jwtlib.SigningMethodRS256 {
			return nil, ErrInvalidToken
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, ErrInvalidToken
		}
		key, lookupErr := m.config.KeySet.Lookup(kid)
		if lookupErr != nil || key == nil {
			return nil, ErrInvalidToken
		}
		return key, nil
	}, jwtlib.WithIssuer(m.config.Issuer), jwtlib.WithExpirationRequired())
	if err != nil || parsed == nil || !parsed.Valid {
		if errors.Is(err, ErrInvalidToken) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	return claims, nil
}
