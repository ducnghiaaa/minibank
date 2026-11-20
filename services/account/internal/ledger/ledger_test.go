package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ducnghiaaa/minibank/internal/platform/dbtest"
	"github.com/ducnghiaaa/minibank/services/account/internal/db"
	"github.com/ducnghiaaa/minibank/services/account/internal/ledger"
)

func setup(t *testing.T) (*pgxpool.Pool, int64, int64) {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var bank, merchant int64
	pool.QueryRow(ctx, `SELECT id FROM accounts WHERE type = 'SYSTEM'`).Scan(&bank)
	pool.QueryRow(ctx, `SELECT id FROM accounts WHERE type = 'MERCHANT' ORDER BY id LIMIT 1`).Scan(&merchant)
	t.Cleanup(func() {
		if err := ledger.CheckInvariants(ctx, pool); err != nil {
			t.Error(err)
		}
	})
	return pool, bank, merchant
}

func post(ctx context.Context, pool *pgxpool.Pool, e ledger.Entry) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	id, err := ledger.Post(ctx, tx, e)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func balance(t *testing.T, pool *pgxpool.Pool, id int64) int64 {
	var b int64
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM balances WHERE account_id = $1`, id).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPostMovesMoneyAndWritesEvent(t *testing.T) {
	ctx := context.Background()
	pool, bank, merchant := setup(t)

	_, err := post(ctx, pool, ledger.Entry{Type: "OPENING", From: bank, To: merchant, Amount: 5000,
		Initiator: merchant, IdempotencyKey: "k1", RequestHash: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := balance(t, pool, merchant); got != 5000 {
		t.Errorf("merchant balance = %d, want 5000", got)
	}
	if got := balance(t, pool, bank); got != -5000 {
		t.Errorf("bank balance = %d, want -5000", got)
	}

	var events int
	pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE payload->>'amount' = '5000'`).Scan(&events)
	if events != 1 {
		t.Errorf("outbox events = %d, want 1", events)
	}
}

func TestInsufficientFundsWritesNothing(t *testing.T) {
	ctx := context.Background()
	pool, bank, merchant := setup(t)

	_, err := post(ctx, pool, ledger.Entry{Type: "PAYMENT", From: merchant, To: bank, Amount: 1,
		Initiator: merchant, IdempotencyKey: "k1", RequestHash: "h1"})
	if !errors.Is(err, ledger.ErrInsufficientFunds) {
		t.Fatalf("err = %v, want ErrInsufficientFunds", err)
	}
	var n int
	pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM transactions) + (SELECT count(*) FROM outbox)`).Scan(&n)
	if n != 0 {
		t.Errorf("rows written = %d, want 0", n)
	}
}

func TestInvalidEntry(t *testing.T) {
	ctx := context.Background()
	pool, bank, _ := setup(t)
	for _, e := range []ledger.Entry{
		{Type: "TRANSFER", From: bank, To: bank, Amount: 100, Initiator: bank, IdempotencyKey: "a", RequestHash: "h"},
		{Type: "TRANSFER", From: bank, To: bank + 1, Amount: 0, Initiator: bank, IdempotencyKey: "b", RequestHash: "h"},
	} {
		if _, err := post(ctx, pool, e); err == nil {
			t.Errorf("entry %+v accepted, want error", e)
		}
	}
}
