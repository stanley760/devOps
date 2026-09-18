package middleware

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	securityauthz "devops-infra/security/authz"
	securityjwt "devops-infra/security/jwt"
)

const ClaimsContextKey = "security.claims"

type GinOptions struct {
	OnUnauthorized        func(c *gin.Context, err error)
	OnForbidden           func(c *gin.Context, err error)
	OnInfrastructureError func(c *gin.Context, err error)
}

func CurrentClaims(c *gin.Context) *securityjwt.Claims {
	if c == nil {
		return nil
	}
	value, ok := c.Get(ClaimsContextKey)
	if !ok {
		return nil
	}
	claims, _ := value.(*securityjwt.Claims)
	return claims
}

func GinAuthenticate(authenticator *Authenticator, opts GinOptions) gin.HandlerFunc {
	opts = withDefaults(opts)

	return func(c *gin.Context) {
		claims, err := authenticator.Authenticate(c.Request.Context(), c.GetHeader("Authorization"))
		if err != nil {
			if errors.Is(err, ErrInfrastructure) {
				opts.OnInfrastructureError(c, err)
			} else {
				opts.OnUnauthorized(c, err)
			}
			c.Abort()
			return
		}

		c.Set(ClaimsContextKey, claims)
		c.Next()
	}
}

func GinRequirePermission(authorizer securityauthz.Authorizer, permission string, opts GinOptions) gin.HandlerFunc {
	opts = withDefaults(opts)

	return func(c *gin.Context) {
		claims := CurrentClaims(c)
		if claims == nil {
			opts.OnUnauthorized(c, ErrUnauthorized)
			c.Abort()
			return
		}

		ok, err := authorizer.Enforce(
			c.Request.Context(),
			strconv.FormatInt(claims.UID, 10),
			strconv.FormatInt(claims.TenantID, 10),
			permission,
		)
		if err != nil {
			opts.OnInfrastructureError(c, err)
			c.Abort()
			return
		}
		if !ok {
			opts.OnForbidden(c, ErrForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}

func GinRequirePlatformPermission(authorizer securityauthz.Authorizer, permission string, opts GinOptions) gin.HandlerFunc {
	opts = withDefaults(opts)

	return func(c *gin.Context) {
		claims := CurrentClaims(c)
		if claims == nil {
			opts.OnUnauthorized(c, ErrUnauthorized)
			c.Abort()
			return
		}

		subject := strconv.FormatInt(claims.UID, 10)
		isAdmin, err := authorizer.IsPlatformAdmin(c.Request.Context(), subject)
		if err != nil {
			opts.OnInfrastructureError(c, err)
			c.Abort()
			return
		}
		if !isAdmin {
			opts.OnForbidden(c, ErrForbidden)
			c.Abort()
			return
		}

		ok, err := authorizer.EnforcePlatform(c.Request.Context(), subject, permission)
		if err != nil {
			opts.OnInfrastructureError(c, err)
			c.Abort()
			return
		}
		if !ok {
			opts.OnForbidden(c, ErrForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}

func withDefaults(opts GinOptions) GinOptions {
	if opts.OnUnauthorized == nil {
		opts.OnUnauthorized = func(c *gin.Context, err error) {
			c.Status(http.StatusUnauthorized)
		}
	}
	if opts.OnForbidden == nil {
		opts.OnForbidden = func(c *gin.Context, err error) {
			c.Status(http.StatusForbidden)
		}
	}
	if opts.OnInfrastructureError == nil {
		opts.OnInfrastructureError = func(c *gin.Context, err error) {
			c.Status(http.StatusServiceUnavailable)
		}
	}
	return opts
}
