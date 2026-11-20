// Package txntoken verifies transaction tokens (app spec §6.3): short-lived ES256 JWTs that the
// security service signs after checking the PIN. A token is bound to one exact transaction.
package txntoken

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the transaction a token approves.
type Claims struct {
	Sub            string
	JTI            string
	ExpiresAt      time.Time
	Type           string
	ToAccount      string
	Amount         int64
	IdempotencyKey string
}

// Verifier checks tokens against the security service's JWKS.
type Verifier struct {
	Issuer  string
	Keyfunc jwt.Keyfunc
}

type txn struct {
	Type           string `json:"type"`
	ToAccount      string `json:"to_account"`
	Amount         int64  `json:"amount"`
	IdempotencyKey string `json:"idempotency_key"`
}

type tokenClaims struct {
	jwt.RegisteredClaims
	Txn *txn `json:"txn"`
}

// Verify checks signature (ES256 only), issuer, audience=account, expiry, and that jti and txn exist.
func (v Verifier) Verify(raw string) (Claims, error) {
	var tc tokenClaims
	_, err := jwt.ParseWithClaims(raw, &tc, v.Keyfunc,
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithIssuer(v.Issuer),
		jwt.WithAudience("account"),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Claims{}, err
	}
	if tc.ID == "" || tc.Subject == "" || tc.Txn == nil {
		return Claims{}, errors.New("missing jti, sub or txn")
	}
	return Claims{
		Sub: tc.Subject, JTI: tc.ID, ExpiresAt: tc.ExpiresAt.Time,
		Type: tc.Txn.Type, ToAccount: tc.Txn.ToAccount, Amount: tc.Txn.Amount, IdempotencyKey: tc.Txn.IdempotencyKey,
	}, nil
}
