// Package transfers moves money between customers (app spec §4.2 and the algorithm in §5.2).
package transfers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
	"github.com/ducnghiaaa/minibank/services/account/internal/ledger"
	"github.com/ducnghiaaa/minibank/services/account/internal/txntoken"
)

// Limits (app spec §4.2), VND.
const (
	MinAmount   = 1_000
	MaxAmount   = 50_000_000
	DailyLimit  = 100_000_000
	MaxNoteLen  = 140
	MaxKeyLen   = 100
	dayTimeZone = "Asia/Ho_Chi_Minh"
)

// Errors the API maps to HTTP status codes.
var (
	ErrInvalid           = errors.New("invalid request")
	ErrNoAccount         = errors.New("sender has no account")
	ErrRecipientNotFound = errors.New("recipient not found")
	ErrSelfTransfer      = errors.New("cannot transfer to yourself")
	ErrDailyLimit        = errors.New("daily limit exceeded")
	ErrInsufficientFunds = ledger.ErrInsufficientFunds
	ErrIdempotencyReused = errors.New("idempotency key reused with a different request")
	ErrTokenInvalid      = errors.New("transaction token invalid or does not match the request")
	ErrTokenAlreadyUsed  = errors.New("transaction token already used")
)

// Request is one transfer as the customer asked for it.
type Request struct {
	Sub            string // from the access token
	ToAccount      string
	Amount         int64
	Note           string
	IdempotencyKey string
	Token          string // X-Transaction-Token
}

// Result is returned for a new transfer and, unchanged, for every retry with the same key.
type Result struct {
	TransactionID int64     `json:"transaction_id"`
	ToAccount     string    `json:"to_account"`
	Amount        int64     `json:"amount"`
	Note          string    `json:"note,omitempty"`
	BalanceAfter  int64     `json:"balance_after"`
	CreatedAt     time.Time `json:"created_at"`
	Replayed      bool      `json:"-"`
}

// TokenVerifier checks a transaction token.
type TokenVerifier interface {
	Verify(raw string) (txntoken.Claims, error)
}

// Service runs transfers.
type Service struct {
	Pool   *pgxpool.Pool
	Tokens TokenVerifier
}

// Transfer runs the §5.2 algorithm in one Postgres transaction.
func (s Service) Transfer(ctx context.Context, r Request) (Result, error) {
	if err := validate(r); err != nil {
		return Result{}, err
	}
	// two parallel retries with the same key: one inserts, the other hits the unique key
	// and, on the second attempt, finds the first one's result
	res, err := s.transfer(ctx, r)
	if pgCode(err) == "23505" {
		res, err = s.transfer(ctx, r)
	}
	return res, err
}

func (s Service) transfer(ctx context.Context, r Request) (Result, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)

	var from int64
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_sub = $1`, r.Sub).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, ErrNoAccount
	}
	if err != nil {
		return Result{}, err
	}

	// 1. idempotency first: a valid retry always gets the old result, even with a used token
	hash := requestHash(r)
	if res, found, err := previous(ctx, tx, from, r.IdempotencyKey, hash); found || err != nil {
		return res, err
	}

	// 2. the token must approve exactly this transfer, by this customer
	c, err := s.Tokens.Verify(r.Token)
	if err != nil || c.Sub != r.Sub || c.Type != "TRANSFER" || c.ToAccount != r.ToAccount ||
		c.Amount != r.Amount || c.IdempotencyKey != r.IdempotencyKey {
		return Result{}, ErrTokenInvalid
	}

	// 3. one token, one use; check the row count instead of failing the whole transaction
	tag, err := tx.Exec(ctx, `INSERT INTO used_jti (jti, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING`, c.JTI, c.ExpiresAt)
	if err != nil {
		return Result{}, err
	}
	if tag.RowsAffected() == 0 {
		// a parallel retry with this token may have just committed: under READ COMMITTED a new
		// statement sees it, so a retry still gets the old result instead of an error
		if res, found, err := previous(ctx, tx, from, r.IdempotencyKey, hash); found || err != nil {
			return res, err
		}
		return Result{}, ErrTokenAlreadyUsed
	}

	var to int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM accounts WHERE number = $1 AND type = 'CUSTOMER' AND status = 'ACTIVE'`, r.ToAccount).Scan(&to)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, ErrRecipientNotFound
	}
	if err != nil {
		return Result{}, err
	}
	if to == from {
		return Result{}, ErrSelfTransfer
	}

	// 4. lock both balances before the limit check, so two parallel transfers cannot both pass it
	if _, err := ledger.Lock(ctx, tx, from, to); err != nil {
		return Result{}, err
	}

	// 5. daily limit on money out (transfers + payments), by Vietnam calendar day
	var spent int64
	err = tx.QueryRow(ctx,
		`SELECT coalesce(-sum(p.amount), 0) FROM postings p JOIN transactions t ON t.id = p.transaction_id
		 WHERE p.account_id = $1 AND p.amount < 0 AND t.type IN ('TRANSFER', 'PAYMENT')
		   AND t.created_at >= date_trunc('day', now() AT TIME ZONE $2) AT TIME ZONE $2`,
		from, dayTimeZone).Scan(&spent)
	if err != nil {
		return Result{}, err
	}
	if spent+r.Amount > DailyLimit {
		return Result{}, ErrDailyLimit
	}

	// 6. move the money; the balance CHECK rejects an overdraft
	if _, err := ledger.Post(ctx, tx, ledger.Entry{
		Type: "TRANSFER", From: from, To: to, Amount: r.Amount, Initiator: from,
		IdempotencyKey: r.IdempotencyKey, RequestHash: hash, Note: r.Note,
	}); err != nil {
		return Result{}, err
	}
	// read back through the same query a retry uses, so both always return the same shape
	res, _, err := previous(ctx, tx, from, r.IdempotencyKey, hash)
	if err != nil {
		return Result{}, err
	}
	res.Replayed = false

	// 7. commit
	return res, tx.Commit(ctx)
}

// previous returns the stored result for (initiator, key), if any.
func previous(ctx context.Context, tx pgx.Tx, from int64, key, hash string) (Result, bool, error) {
	var res Result
	var storedHash string
	err := tx.QueryRow(ctx,
		`SELECT t.id, t.request_hash, t.amount, coalesce(t.note, ''), t.created_at, p.balance_after, a.number
		 FROM transactions t
		 JOIN postings p ON p.transaction_id = t.id AND p.account_id = t.initiator_account_id
		 JOIN postings q ON q.transaction_id = t.id AND q.account_id <> t.initiator_account_id
		 JOIN accounts a ON a.id = q.account_id
		 WHERE t.initiator_account_id = $1 AND t.idempotency_key = $2`, from, key).
		Scan(&res.TransactionID, &storedHash, &res.Amount, &res.Note, &res.CreatedAt, &res.BalanceAfter, &res.ToAccount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if storedHash != hash {
		return Result{}, true, ErrIdempotencyReused
	}
	res.Replayed = true
	return res, true, nil
}

func validate(r Request) error {
	switch {
	case r.IdempotencyKey == "" || len(r.IdempotencyKey) > MaxKeyLen:
		return fmt.Errorf("%w: Idempotency-Key is required (max %d chars)", ErrInvalid, MaxKeyLen)
	case r.Amount < MinAmount || r.Amount > MaxAmount:
		return fmt.Errorf("%w: amount must be between %d and %d", ErrInvalid, MinAmount, MaxAmount)
	case len([]rune(r.Note)) > MaxNoteLen:
		return fmt.Errorf("%w: note is longer than %d characters", ErrInvalid, MaxNoteLen)
	case !accounts.ValidNumber(r.ToAccount):
		return fmt.Errorf("%w: to_account is not a valid account number", ErrInvalid)
	case r.Token == "":
		return fmt.Errorf("%w: X-Transaction-Token is required", ErrInvalid)
	}
	return nil
}

// requestHash fingerprints what the customer asked for, to tell a retry from a different request.
func requestHash(r Request) string {
	h := sha256.Sum256(fmt.Appendf(nil, "TRANSFER\x00%s\x00%d\x00%s", r.ToAccount, r.Amount, r.Note))
	return hex.EncodeToString(h[:])
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
