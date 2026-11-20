package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "https://issuer.test"

func setup(t *testing.T) (*rsa.PrivateKey, Verifier) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kf := func(*jwt.Token) (any, error) { return &key.PublicKey, nil }
	return key, Verifier{Issuer: issuer, Keyfunc: kf}
}

func sign(t *testing.T, key *rsa.PrivateKey, method jwt.SigningMethod, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func valid() jwt.MapClaims {
	return jwt.MapClaims{
		"iss": issuer, "sub": "sub-1", "token_use": "access", "name": "Nguyễn Văn An",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func call(v Verifier, header string) (int, Claims) {
	var got Claims
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = FromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got
}

func TestValidToken(t *testing.T) {
	key, v := setup(t)
	code, c := call(v, "Bearer "+sign(t, key, jwt.SigningMethodRS256, valid()))
	if code != http.StatusOK || c.Sub != "sub-1" || c.Name != "Nguyễn Văn An" {
		t.Errorf("got %d %+v", code, c)
	}
}

func TestRejected(t *testing.T) {
	key, v := setup(t)
	with := func(k string, val any) jwt.MapClaims { c := valid(); c[k] = val; return c }
	cases := map[string]string{
		"no header":    "",
		"not bearer":   "Basic abc",
		"garbage":      "Bearer not-a-jwt",
		"wrong issuer": "Bearer " + sign(t, key, jwt.SigningMethodRS256, with("iss", "https://evil.test")),
		"expired":      "Bearer " + sign(t, key, jwt.SigningMethodRS256, with("exp", time.Now().Add(-time.Minute).Unix())),
		"id token":     "Bearer " + sign(t, key, jwt.SigningMethodRS256, with("token_use", "id")),
		"wrong alg":    "Bearer " + sign(t, key, jwt.SigningMethodRS512, valid()),
		"no expiry":    "Bearer " + sign(t, key, jwt.SigningMethodRS256, with("exp", nil)),
	}
	for name, header := range cases {
		if code, _ := call(v, header); code != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", name, code)
		}
	}
}
