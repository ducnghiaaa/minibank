// Package auth checks the customer's access token (Cognito in AWS, devtoken locally).
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is what handlers need to know about the caller.
type Claims struct {
	Sub  string
	Name string // from the token when present (devtoken); Cognito userInfo comes later
}

type ctxKey struct{}

// WithClaims stores c in ctx. Used by the middleware and by handler tests.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext returns the caller's claims; ok is false if the request was not authenticated.
func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}

// Verifier checks access tokens signed by keys from a JWKS.
type Verifier struct {
	Issuer  string
	Keyfunc jwt.Keyfunc
}

type tokenClaims struct {
	jwt.RegisteredClaims
	TokenUse string `json:"token_use"`
	Name     string `json:"name"`
}

var errNotAccess = errors.New("not an access token")

// Verify parses and checks a raw token: signature, RS256, issuer, expiry, token_use=access.
func (v Verifier) Verify(raw string) (Claims, error) {
	var tc tokenClaims
	_, err := jwt.ParseWithClaims(raw, &tc, v.Keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.Issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Claims{}, err
	}
	// Cognito ID tokens are signed by the same keys; only access tokens may call the API
	if tc.TokenUse != "access" {
		return Claims{}, errNotAccess
	}
	if tc.Subject == "" {
		return Claims{}, errors.New("missing sub")
	}
	return Claims{Sub: tc.Subject, Name: tc.Name}, nil
}

// Middleware rejects requests without a valid Bearer token with 401.
func (v Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		c, err := v.Verify(raw)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), c)))
	})
}
