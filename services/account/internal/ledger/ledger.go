// Package ledger is the only code that changes balances (app spec §5.1–5.2).
// Every money movement is one transaction with postings that sum to 0.
package ledger

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrUnbalanced        = errors.New("postings do not sum to 0")
)

// Entry is one money movement: From pays Amount to To.
type Entry struct {
	Type           string // OPENING, TRANSFER, PAYMENT
	From, To       int64  // account ids
	Amount         int64  // VND, > 0
	Initiator      int64
	IdempotencyKey string
	RequestHash    string
	Category       string
	Note           string
}

type leg struct {
	account int64
	amount  int64
}

// Post writes the entry inside tx: locks both balances, writes the transaction,
// two postings, the new balances and one outbox event. The caller commits.
func Post(ctx context.Context, tx pgx.Tx, e Entry) (int64, error) {
	if e.Amount <= 0 || e.From == e.To {
		return 0, fmt.Errorf("invalid entry: amount=%d from=%d to=%d", e.Amount, e.From, e.To)
	}
	legs := []leg{{e.From, -e.Amount}, {e.To, e.Amount}}
	if sum(legs) != 0 {
		return 0, ErrUnbalanced
	}

	balances, err := Lock(ctx, tx, e.From, e.To)
	if err != nil {
		return 0, err
	}

	var txID int64
	var createdAt time.Time
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (type, initiator_account_id, idempotency_key, request_hash, amount, category, note)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, '')) RETURNING id, created_at`,
		e.Type, e.Initiator, e.IdempotencyKey, e.RequestHash, e.Amount, e.Category, e.Note).Scan(&txID, &createdAt)
	if err != nil {
		return 0, err
	}

	for _, l := range legs {
		after := balances[l.account] + l.amount
		if _, err := tx.Exec(ctx,
			`UPDATE balances SET balance = $2, updated_at = now() WHERE account_id = $1`, l.account, after); err != nil {
			// the CHECK on balances is the last line of defence
			if pgCode(err) == "23514" {
				return 0, ErrInsufficientFunds
			}
			return 0, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO postings (transaction_id, account_id, amount, balance_after) VALUES ($1, $2, $3, $4)`,
			txID, l.account, l.amount, after); err != nil {
			return 0, err
		}
	}

	if err := writeEvent(ctx, tx, txID, e, createdAt); err != nil {
		return 0, err
	}
	return txID, nil
}

// Lock takes row locks on the balances of ids, always in ascending account_id order,
// so two opposite transfers never deadlock. Callers that must check something before
// Post (e.g. a daily limit) lock first; Post locking again in the same tx is a no-op.
func Lock(ctx context.Context, tx pgx.Tx, ids ...int64) (map[int64]int64, error) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	rows, err := tx.Query(ctx,
		`SELECT account_id, balance FROM balances WHERE account_id = ANY($1) ORDER BY account_id FOR UPDATE`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	balances := make(map[int64]int64, len(ids))
	for rows.Next() {
		var id, bal int64
		if err := rows.Scan(&id, &bal); err != nil {
			return nil, err
		}
		balances[id] = bal
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(balances) != len(slices.Compact(ids)) {
		return nil, errors.New("account not found")
	}
	return balances, nil
}

// TransactionCompleted, app spec §7.1
type event struct {
	EventID       string  `json:"event_id"`
	TransactionID int64   `json:"transaction_id"`
	Type          string  `json:"type"`
	FromAccount   string  `json:"from_account"`
	FromOwnerSub  *string `json:"from_owner_sub"`
	ToAccount     string  `json:"to_account"`
	ToOwnerSub    *string `json:"to_owner_sub"`
	Amount        int64   `json:"amount"`
	Category      string  `json:"category,omitempty"`
	Note          string  `json:"note,omitempty"`
	OccurredAt    string  `json:"occurred_at"`
}

func writeEvent(ctx context.Context, tx pgx.Tx, txID int64, e Entry, at time.Time) error {
	ev := event{
		EventID: newUUID(), TransactionID: txID, Type: e.Type, Amount: e.Amount,
		Category: e.Category, Note: e.Note, OccurredAt: at.UTC().Format(time.RFC3339Nano),
	}
	q := `SELECT number, owner_sub FROM accounts WHERE id = $1`
	if err := tx.QueryRow(ctx, q, e.From).Scan(&ev.FromAccount, &ev.FromOwnerSub); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, q, e.To).Scan(&ev.ToAccount, &ev.ToOwnerSub); err != nil {
		return err
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO outbox (event_id, type, payload) VALUES ($1, 'TransactionCompleted', $2)`, ev.EventID, payload)
	return err
}

func sum(legs []leg) int64 {
	var s int64
	for _, l := range legs {
		s += l.amount
	}
	return s
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
