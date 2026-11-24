// Package accounts opens customer accounts lazily, on the customer's first request (app spec §4.1).
package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/ducnghiaaa/minibank/services/account/internal/ledger"
)

// OpeningGift is moved from BANK_FUNDING into every new account.
const OpeningGift = 10_000_000

// Account is what a customer sees about their own account.
type Account struct {
	ID          int64  `json:"-"`
	Number      string `json:"number"`
	DisplayName string `json:"display_name"`
	Balance     int64  `json:"balance"`
}

// ErrNotFound means no active customer account has that number.
var ErrNotFound = errors.New("account not found")

// Recipient is what a sender may see before confirming a transfer: never the full name.
type Recipient struct {
	Number     string `json:"number"`
	MaskedName string `json:"masked_name"`
}

// NameSource finds a customer's name from their access token (Cognito userInfo).
type NameSource interface {
	Name(ctx context.Context, accessToken string) (string, error)
}

// Service opens and reads customer accounts.
type Service struct {
	Pool  *pgxpool.Pool
	Names NameSource // optional: used when the token itself carries no name
}

// Me returns the caller's account, opening it (with the gift) if this is their first request.
// name may be empty: then, on first open only, it is fetched with token from s.Names.
// Safe to call many times in parallel for the same sub: exactly one account and one gift.
func (s Service) Me(ctx context.Context, sub, name, token string) (Account, error) {
	if a, err := s.get(ctx, sub); err == nil {
		return a, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Account{}, err
	}
	if name == "" && s.Names != nil {
		// the name is stored once and never changes in phase 1, so fail rather than store a wrong one
		n, err := s.Names.Name(ctx, token)
		if err != nil {
			return Account{}, fmt.Errorf("customer name: %w", err)
		}
		name = n
	}
	// a random number can collide with an existing one; try a few times
	for range 5 {
		if err := s.open(ctx, sub, displayName(name)); err != nil {
			return Account{}, err
		}
		a, err := s.get(ctx, sub)
		if !errors.Is(err, pgx.ErrNoRows) {
			return a, err
		}
		// not there: the random number was taken, so nothing was inserted; try another
	}
	return Account{}, errors.New("could not allocate an account number")
}

// Lookup lets a sender check who owns an account number before sending money.
// Only customer accounts; the caller checks the number format first (ValidNumber).
func (s Service) Lookup(ctx context.Context, number string) (Recipient, error) {
	var name string
	err := s.Pool.QueryRow(ctx,
		`SELECT display_name FROM accounts WHERE number = $1 AND type = 'CUSTOMER' AND status = 'ACTIVE'`, number).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Recipient{}, ErrNotFound
	}
	if err != nil {
		return Recipient{}, err
	}
	return Recipient{Number: number, MaskedName: MaskName(name)}, nil
}

// MaskName keeps the family name and initials: "NGUYEN VAN AN" → "NGUYEN V. A".
// Enough to spot a wrong recipient, not enough to learn someone's full name.
func MaskName(name string) string {
	words := strings.Fields(name)
	switch len(words) {
	case 0:
		return ""
	case 1:
		return firstLetter(words[0])
	}
	out := []string{words[0]}
	for _, w := range words[1 : len(words)-1] {
		out = append(out, firstLetter(w)+".")
	}
	return strings.Join(append(out, firstLetter(words[len(words)-1])), " ")
}

func firstLetter(w string) string {
	for _, r := range w {
		return string(r)
	}
	return ""
}

func (s Service) get(ctx context.Context, sub string) (Account, error) {
	var a Account
	err := s.Pool.QueryRow(ctx,
		`SELECT a.id, a.number, a.display_name, b.balance
		 FROM accounts a JOIN balances b ON b.account_id = a.id WHERE a.owner_sub = $1`, sub).
		Scan(&a.ID, &a.Number, &a.DisplayName, &a.Balance)
	return a, err
}

// open inserts the account, its balance and the OPENING transaction in one DB transaction.
// On a conflict (another request opened it first, or the number is taken) it inserts nothing.
func (s Service) open(ctx context.Context, sub, name string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	number, err := newNumber()
	if err != nil {
		return err
	}
	var id int64
	// a parallel request for the same sub waits on the unique index, then does nothing
	err = tx.QueryRow(ctx,
		`INSERT INTO accounts (number, type, owner_sub, display_name) VALUES ($1, 'CUSTOMER', $2, $3)
		 ON CONFLICT DO NOTHING RETURNING id`, number, sub, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO balances (account_id) VALUES ($1)`, id); err != nil {
		return err
	}

	var bank int64
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE type = 'SYSTEM' AND display_name = 'BANK_FUNDING'`).Scan(&bank); err != nil {
		return fmt.Errorf("BANK_FUNDING: %w", err)
	}
	if _, err := ledger.Post(ctx, tx, ledger.Entry{
		Type: "OPENING", From: bank, To: id, Amount: OpeningGift,
		Initiator: id, IdempotencyKey: "opening", RequestHash: "opening",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// newNumber returns 9 random digits plus a Luhn check digit, which catches one mistyped digit.
func newNumber() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("%09d", n)
	// 0… is reserved for system accounts, 9… for merchants
	if base[0] == '0' || base[0] == '9' {
		base = "1" + base[1:]
	}
	return base + luhnDigit(base), nil
}

func luhnDigit(s string) string {
	sum := 0
	for i := len(s) - 1; i >= 0; i-- {
		d := int(s[i] - '0')
		if (len(s)-1-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return fmt.Sprint((10 - sum%10) % 10)
}

// ValidNumber reports whether s is 10 digits with a correct Luhn check digit.
func ValidNumber(s string) bool {
	if len(s) != 10 || strings.Trim(s, "0123456789") != "" {
		return false
	}
	return luhnDigit(s[:9]) == s[9:]
}

// displayName turns "Nguyễn Văn An" into "NGUYEN VAN AN".
func displayName(name string) string {
	name = strings.ReplaceAll(strings.ReplaceAll(name, "đ", "d"), "Đ", "D")
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	plain, _, err := transform.String(t, name)
	if err != nil {
		plain = name
	}
	plain = strings.ToUpper(strings.Join(strings.Fields(plain), " "))
	if plain == "" {
		return "KHACH HANG"
	}
	return plain
}
