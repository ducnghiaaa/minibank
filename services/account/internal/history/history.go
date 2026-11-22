// Package history lists a customer's own transactions, newest first (app spec §4.4).
package history

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

var (
	ErrInvalid   = errors.New("invalid request")
	ErrNoAccount = errors.New("caller has no account")
)

// Query is one page request. Zero values mean "no filter".
type Query struct {
	Sub       string
	Cursor    string    // from the previous page's NextCursor
	Limit     int       // 1..MaxLimit, 0 = DefaultLimit
	From, To  time.Time // created_at in [From, To)
	Direction string    // "", "in" or "out"
	Category  string
}

// Item is one line of the statement, seen from the caller's account.
type Item struct {
	TransactionID      int64     `json:"transaction_id"`
	Type               string    `json:"type"`
	Direction          string    `json:"direction"`
	Amount             int64     `json:"amount"` // signed: + money in, - money out
	CounterpartyNumber string    `json:"counterparty_number"`
	CounterpartyName   string    `json:"counterparty_name"` // masked for customers
	Category           string    `json:"category,omitempty"`
	Note               string    `json:"note,omitempty"`
	BalanceAfter       int64     `json:"balance_after"`
	CreatedAt          time.Time `json:"created_at"`
}

// Page is one page of items and the cursor for the next one ("" when this is the last page).
type Page struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Service reads history.
type Service struct {
	Pool *pgxpool.Pool
}

// List returns one page. Pages are keyed on the posting id (keyset pagination, not OFFSET):
// new transactions get larger ids, so they never shift or repeat rows on later pages.
func (s Service) List(ctx context.Context, q Query) (Page, error) {
	limit, after, err := q.validate()
	if err != nil {
		return Page{}, err
	}

	var account int64
	err = s.Pool.QueryRow(ctx, `SELECT id FROM accounts WHERE owner_sub = $1`, q.Sub).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNoAccount
	}
	if err != nil {
		return Page{}, err
	}

	// fetch one extra row to know whether another page exists
	rows, err := s.Pool.Query(ctx,
		`SELECT p.id, t.id, t.type, p.amount, other.number, other.type, other.display_name,
		        coalesce(t.category, ''), coalesce(t.note, ''), p.balance_after, t.created_at
		 FROM postings p
		 JOIN transactions t ON t.id = p.transaction_id
		 JOIN postings op    ON op.transaction_id = t.id AND op.account_id <> p.account_id
		 JOIN accounts other ON other.id = op.account_id
		 WHERE p.account_id = $1
		   AND ($2 = 0 OR p.id < $2)
		   AND ($3::timestamptz IS NULL OR t.created_at >= $3)
		   AND ($4::timestamptz IS NULL OR t.created_at < $4)
		   AND ($5 = '' OR ($5 = 'in' AND p.amount > 0) OR ($5 = 'out' AND p.amount < 0))
		   AND ($6 = '' OR t.category = $6)
		 ORDER BY p.id DESC
		 LIMIT $7`,
		account, after, nullTime(q.From), nullTime(q.To), q.Direction, q.Category, limit+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Items: []Item{}}
	var lastID int64
	for rows.Next() {
		var it Item
		var postingID int64
		var otherType string
		if err := rows.Scan(&postingID, &it.TransactionID, &it.Type, &it.Amount, &it.CounterpartyNumber, &otherType,
			&it.CounterpartyName, &it.Category, &it.Note, &it.BalanceAfter, &it.CreatedAt); err != nil {
			return Page{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(lastID)
			break
		}
		it.Direction = "in"
		if it.Amount < 0 {
			it.Direction = "out"
		}
		// merchants and the bank are public names; other customers are masked
		if otherType == "CUSTOMER" {
			it.CounterpartyName = accounts.MaskName(it.CounterpartyName)
		}
		page.Items = append(page.Items, it)
		lastID = postingID
	}
	return page, rows.Err()
}

func (q Query) validate() (limit int, after int64, err error) {
	limit = q.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return 0, 0, fmt.Errorf("%w: limit must be 1..%d", ErrInvalid, MaxLimit)
	}
	if q.Direction != "" && q.Direction != "in" && q.Direction != "out" {
		return 0, 0, fmt.Errorf("%w: direction must be in or out", ErrInvalid)
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return 0, 0, fmt.Errorf("%w: from must be before to", ErrInvalid)
	}
	if q.Cursor != "" {
		if after, err = decodeCursor(q.Cursor); err != nil {
			return 0, 0, fmt.Errorf("%w: bad cursor", ErrInvalid)
		}
	}
	return limit, after, nil
}

// the cursor is opaque to clients so its format can change later
func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("p" + strconv.FormatInt(id, 10)))
}

func decodeCursor(c string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || len(b) < 2 || b[0] != 'p' {
		return 0, errors.New("bad cursor")
	}
	id, err := strconv.ParseInt(string(b[1:]), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("bad cursor")
	}
	return id, nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
