package txntoken

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "https://security.test"

func claims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss": issuer, "aud": "account", "sub": "sub-1", "jti": "j-1",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
		"txn": map[string]any{"type": "TRANSFER", "to_account": "7020113549", "amount": 500000, "idempotency_key": "k-1"},
	}
}

func TestVerify(t *testing.T) {
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rs, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := Verifier{Issuer: issuer, Keyfunc: func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); ok {
			return &ec.PublicKey, nil
		}
		return &rs.PublicKey, nil
	}}
	sign := func(m jwt.SigningMethod, key any, c jwt.MapClaims) string {
		s, err := jwt.NewWithClaims(m, c).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	got, err := v.Verify(sign(jwt.SigningMethodES256, ec, claims()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Sub != "sub-1" || got.JTI != "j-1" || got.Amount != 500000 || got.ToAccount != "7020113549" || got.IdempotencyKey != "k-1" {
		t.Errorf("claims = %+v", got)
	}

	with := func(k string, val any) jwt.MapClaims { c := claims(); c[k] = val; return c }
	bad := map[string]string{
		"RS256 (access-token key)": sign(jwt.SigningMethodRS256, rs, claims()),
		"wrong audience":           sign(jwt.SigningMethodES256, ec, with("aud", "security")),
		"wrong issuer":             sign(jwt.SigningMethodES256, ec, with("iss", "https://evil.test")),
		"expired":                  sign(jwt.SigningMethodES256, ec, with("exp", time.Now().Add(-time.Second).Unix())),
		"no jti":                   sign(jwt.SigningMethodES256, ec, with("jti", nil)),
		"no txn":                   sign(jwt.SigningMethodES256, ec, with("txn", nil)),
	}
	for name, raw := range bad {
		if _, err := v.Verify(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
