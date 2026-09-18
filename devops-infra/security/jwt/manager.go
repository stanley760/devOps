package jwt

import (
	"context"
	"errors"
	"strconv"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

var ErrInvalidConfig = errors.New("jwt: invalid config")
var ErrInvalidToken = errors.New("jwt: invalid token")

// Manager issues and parses access tokens.
type Manager struct {
	config Config
}

func NewManager(config Config) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Manager{config: config}, nil
}

func (m *Manager) Issue(ctx context.Context, subject Subject, sessionID string) (TokenPair, error) {
	if m == nil {
		return TokenPair{}, ErrInvalidConfig
	}
	if err := m.config.Validate(); err != nil {
		return TokenPair{}, err
	}
	_ = ctx

	now := time.Now()
	claims := Claims{
		UID:        subject.UserID,
		Username:   subject.Username,
		SessionID:  sessionID,
		TenantID:   subject.TenantID,
		TenantCode: subject.TenantCode,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    m.config.Issuer,
			Subject:   strconv.FormatInt(subject.UserID, 10),
			IssuedAt:  jwtlib.NewNumericDate(now),
			NotBefore: jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(m.config.AccessExpire)),
		},
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(m.config.SigningKey))
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:     signed,
		SessionID:       sessionID,
		AccessExpiresIn: int64(m.config.AccessExpire.Seconds()),
	}, nil
}

func (m *Manager) ParseAccess(tokenString string) (*Claims, error) {
	if m == nil {
		return nil, ErrInvalidConfig
	}
	if err := m.config.Validate(); err != nil {
		return nil, err
	}
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	claims := new(Claims)
	parsed, err := jwtlib.ParseWithClaims(tokenString, claims, func(token *jwtlib.Token) (any, error) {
		if token.Method.Alg() != jwtlib.SigningMethodHS256.Alg() {
			return nil, ErrInvalidToken
		}
		return []byte(m.config.SigningKey), nil
	}, jwtlib.WithIssuer(m.config.Issuer))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
