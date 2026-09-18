package jwt

import (
	"crypto/rsa"
	"errors"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

var ErrInvalidConfig = errors.New("jwt: invalid config")
var ErrInvalidToken = errors.New("jwt: invalid token")

// KeySet resolves public RSA keys by the key id in a token header.
type KeySet interface {
	Lookup(kid string) (*rsa.PublicKey, error)
}

// Config holds the signing key and token validation settings.
type Config struct {
	PrivateKeyPEM []byte
	KeyID         string
	Issuer        string
	AccessExpire  time.Duration
	RefreshExpire time.Duration
	KeySet        KeySet
}

func (c Config) Validate() error {
	if len(c.PrivateKeyPEM) == 0 || c.KeyID == "" || c.Issuer == "" ||
		c.AccessExpire <= 0 || c.RefreshExpire <= 0 || c.KeySet == nil {
		return ErrInvalidConfig
	}
	return nil
}

// Subject represents the authenticated principal for a token.
type Subject struct {
	UserID     int64
	Username   string
	TenantID   int64
	TenantCode string
}

// Claims contains the shared access-token identity fields and standard JWT metadata.
type Claims struct {
	UID        int64  `json:"uid"`
	Username   string `json:"username"`
	SessionID  string `json:"sid"`
	TenantID   int64  `json:"tid"`
	TenantCode string `json:"tcode"`
	jwtlib.RegisteredClaims
}

// TokenPair is returned when issuing a new access token.
type TokenPair struct {
	AccessToken     string `json:"access_token"`
	SessionID       string `json:"session_id"`
	AccessExpiresIn int64  `json:"access_expires_in"`
}
