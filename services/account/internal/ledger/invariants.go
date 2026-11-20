package ledger

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckInvariants verifies the ledger is consistent. Tests call it after every scenario.
//   - every transaction's postings sum to 0
//   - all balances together sum to 0 (BANK_FUNDING is negative by what it gave away)
//   - each balance equals the sum of its postings
func CheckInvariants(ctx context.Context, pool *pgxpool.Pool) error {
	var bad int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM (SELECT transaction_id FROM postings GROUP BY 1 HAVING sum(amount) <> 0) x`).Scan(&bad); err != nil {
		return err
	}
	if bad > 0 {
		return fmt.Errorf("%d transactions do not sum to 0", bad)
	}

	var total int64
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(balance), 0) FROM balances`).Scan(&total); err != nil {
		return err
	}
	if total != 0 {
		return fmt.Errorf("balances sum to %d, want 0", total)
	}

	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM balances b
		 WHERE b.balance <> (SELECT coalesce(sum(p.amount), 0) FROM postings p WHERE p.account_id = b.account_id)`).Scan(&bad); err != nil {
		return err
	}
	if bad > 0 {
		return fmt.Errorf("%d balances differ from their postings", bad)
	}
	return nil
}
