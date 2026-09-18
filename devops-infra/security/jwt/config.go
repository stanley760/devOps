package jwt

import (
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// Config holds the shared JWT signing and expiry settings.
type Config struct {
	SigningKey    string
	Issuer        string
	AccessExpire  time.Duration
	RefreshExpire time.Duration
}

func (c Config) Validate() error {
	if c.SigningKey == "" || c.Issuer == "" || c.AccessExpire <= 0 || c.RefreshExpire <= 0 {
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
