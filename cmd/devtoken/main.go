// Command devtoken is a fake login server for local development, standing in for Cognito.
// It serves a JWKS and issues access tokens with the same claims Cognito uses.
// Never deploy it: anyone can get a token for any user.
//
//	GET /.well-known/jwks.json
//	GET /token?sub=user-1&name=Nguyen+Van+An
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const kid = "devtoken-1"

func main() {
	issuer := os.Getenv("ISSUER") // must match AUTH_ISSUER of the services
	if issuer == "" {
		issuer = "http://localhost:9000"
	}
	// a new key on each start: old tokens stop working after a restart
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})

	http.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		sub := r.URL.Query().Get("sub")
		if sub == "" {
			http.Error(w, "sub is required", http.StatusBadRequest)
			return
		}
		now := time.Now()
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": issuer, "sub": sub, "name": r.URL.Query().Get("name"),
			"token_use": "access", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		})
		tok.Header["kid"] = kid
		s, err := tok.SignedString(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write([]byte(s + "\n"))
	})

	log.Printf("devtoken: issuer %s, listening on :9000", issuer)
	log.Fatal(http.ListenAndServe(":9000", nil))
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
