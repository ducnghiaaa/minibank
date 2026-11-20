// Command devtoken is a fake login and PIN server for local development. It stands in for
// Cognito (access tokens, RS256) and for the security service (transaction tokens, ES256),
// with the same claims both use. Never deploy it: anyone can get any token.
//
//	GET /.well-known/jwks.json                     Cognito-like keys
//	GET /token?sub=user-1&name=Nguyen+Van+An       access token
//	GET /security/.well-known/jwks.json            security-like keys
//	GET /txn-token?sub=user-1&to=7020113549&amount=500000&key=k1
//	                                               transaction token, valid 60s, no PIN asked
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	accessKID = "devtoken-access-1"
	txnKID    = "devtoken-txn-1"
)

func main() {
	issuer := os.Getenv("ISSUER") // must match AUTH_ISSUER; transaction tokens use ISSUER + "/security"
	if issuer == "" {
		issuer = "http://localhost:9000"
	}
	// new keys on each start: old tokens stop working after a restart
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJWKS(w, map[string]string{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": accessKID,
			"n": b64(rsaKey.N.Bytes()), "e": b64(big.NewInt(int64(rsaKey.E)).Bytes()),
		})
	})

	http.HandleFunc("GET /security/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJWKS(w, map[string]string{
			"kty": "EC", "use": "sig", "alg": "ES256", "kid": txnKID, "crv": "P-256",
			"x": b64(ecKey.X.FillBytes(make([]byte, 32))), "y": b64(ecKey.Y.FillBytes(make([]byte, 32))),
		})
	})

	http.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("sub") == "" {
			http.Error(w, "sub is required", http.StatusBadRequest)
			return
		}
		now := time.Now()
		writeToken(w, jwt.SigningMethodRS256, accessKID, rsaKey, jwt.MapClaims{
			"iss": issuer, "sub": q.Get("sub"), "name": q.Get("name"),
			"token_use": "access", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		})
	})

	http.HandleFunc("GET /txn-token", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		amount, err := strconv.ParseInt(q.Get("amount"), 10, 64)
		if q.Get("sub") == "" || q.Get("to") == "" || q.Get("key") == "" || err != nil {
			http.Error(w, "sub, to, amount and key are required", http.StatusBadRequest)
			return
		}
		typ := q.Get("type")
		if typ == "" {
			typ = "TRANSFER"
		}
		now := time.Now()
		writeToken(w, jwt.SigningMethodES256, txnKID, ecKey, jwt.MapClaims{
			"iss": issuer + "/security", "aud": "account", "sub": q.Get("sub"), "jti": randomID(),
			"iat": now.Unix(), "exp": now.Add(60 * time.Second).Unix(),
			"txn": map[string]any{"type": typ, "to_account": q.Get("to"), "amount": amount, "idempotency_key": q.Get("key")},
		})
	})

	log.Printf("devtoken: issuer %s, listening on :9000", issuer)
	log.Fatal(http.ListenAndServe(":9000", nil))
}

func writeJWKS(w http.ResponseWriter, key map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{key}})
}

func writeToken(w http.ResponseWriter, m jwt.SigningMethod, kid string, key any, claims jwt.MapClaims) {
	tok := jwt.NewWithClaims(m, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(s + "\n"))
}

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
