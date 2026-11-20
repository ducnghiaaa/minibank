package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool creates a fresh database for one test and drops it afterwards.
// Needs TEST_DATABASE_URL (e.g. the compose Postgres); skips otherwise.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	adminPool, err := Open(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("account_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(admin)
	u.Path = "/" + name
	pool, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		adminPool.Exec(ctx, "DROP DATABASE "+name)
		adminPool.Close()
	})
	return pool
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestMigrateUpDownUp(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	for _, step := range []func(context.Context, *pgxpool.Pool) error{Migrate, Reset, Migrate} {
		if err := step(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSeed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var system, merchants int
	pool.QueryRow(ctx, `SELECT count(*) FROM accounts a JOIN balances b ON b.account_id = a.id
		WHERE a.type = 'SYSTEM' AND b.allow_negative`).Scan(&system)
	pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE type = 'MERCHANT'`).Scan(&merchants)
	if system != 1 || merchants != 5 {
		t.Errorf("seed: system=%d merchants=%d, want 1 and 5", system, merchants)
	}
}

func TestCustomerBalanceCannotGoNegative(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	var id int64
	err := pool.QueryRow(ctx, `INSERT INTO accounts (number, type, owner_sub, display_name)
		VALUES ('7020113549', 'CUSTOMER', 'sub-1', 'NGUYEN VAN AN') RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO balances (account_id) VALUES ($1)`, id); err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `UPDATE balances SET balance = -1 WHERE account_id = $1`, id)
	if sqlState(err) != "23514" { // check_violation
		t.Errorf("negative balance: got %v, want check_violation", err)
	}
}

func TestIdempotencyKeyIsUnique(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	insert := `INSERT INTO transactions (type, initiator_account_id, idempotency_key, request_hash, amount)
		SELECT 'TRANSFER', id, 'key-1', 'h', 1000 FROM accounts WHERE type = 'SYSTEM'`
	if _, err := pool.Exec(ctx, insert); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, insert)
	if sqlState(err) != "23505" { // unique_violation
		t.Errorf("duplicate key: got %v, want unique_violation", err)
	}
}
