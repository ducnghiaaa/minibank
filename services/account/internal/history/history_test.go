package history

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ducnghiaaa/minibank/internal/platform/dbtest"
	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
	"github.com/ducnghiaaa/minibank/services/account/internal/db"
	"github.com/ducnghiaaa/minibank/services/account/internal/ledger"
)

type env struct {
	pool *pgxpool.Pool
	svc  Service
	a, b accounts.Account
	n    int
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	acc := accounts.Service{Pool: pool}
	a, err := acc.Me(ctx, "sub-a", "Nguyen Van An", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := acc.Me(ctx, "sub-b", "Tran Thi Binh", "")
	if err != nil {
		t.Fatal(err)
	}
	return &env{pool: pool, svc: Service{Pool: pool}, a: a, b: b}
}

// move posts a transfer straight to the ledger (the transfers package has its own tests).
func (e *env) move(t *testing.T, from, to accounts.Account, amount int64, note string) {
	t.Helper()
	ctx := context.Background()
	e.n++
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := ledger.Post(ctx, tx, ledger.Entry{Type: "TRANSFER", From: from.ID, To: to.ID, Amount: amount,
		Initiator: from.ID, IdempotencyKey: fmt.Sprint("k", e.n), RequestHash: "h", Note: note}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func (e *env) all(t *testing.T, q Query) []Item {
	t.Helper()
	var items []Item
	for {
		p, err := e.svc.List(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, p.Items...)
		if p.NextCursor == "" {
			return items
		}
		q.Cursor = p.NextCursor
	}
}

func TestStatement(t *testing.T) {
	e := setup(t)
	e.move(t, e.a, e.b, 100_000, "an trua")
	e.move(t, e.b, e.a, 30_000, "tra lai")

	items := e.all(t, Query{Sub: "sub-a"})
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (opening, out, in)", len(items))
	}
	in, out, opening := items[0], items[1], items[2]
	if in.Direction != "in" || in.Amount != 30_000 || in.BalanceAfter != 9_930_000 || in.CounterpartyName != "TRAN T. B" || in.Note != "tra lai" {
		t.Errorf("newest = %+v", in)
	}
	if out.Direction != "out" || out.Amount != -100_000 || out.BalanceAfter != 9_900_000 || out.CounterpartyNumber != e.b.Number {
		t.Errorf("middle = %+v", out)
	}
	if opening.Type != "OPENING" || opening.Amount != 10_000_000 || opening.CounterpartyName != "BANK_FUNDING" {
		t.Errorf("oldest = %+v", opening)
	}
}

func TestPagesHaveNoGapsOrRepeats(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	for i := range 9 {
		e.move(t, e.a, e.b, int64(1_000*(i+1)), "")
	}
	want := e.all(t, Query{Sub: "sub-a", Limit: 100}) // 10 rows: 9 transfers + opening

	// walk 3 per page, with new transfers arriving between pages
	seen := map[int64]bool{}
	q := Query{Sub: "sub-a", Limit: 3}
	var got []Item
	for {
		p, err := e.svc.List(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			if seen[it.TransactionID] {
				t.Fatalf("transaction %d repeated", it.TransactionID)
			}
			seen[it.TransactionID] = true
		}
		got = append(got, p.Items...)
		e.move(t, e.a, e.b, 1_000, "arrived mid-walk")
		if p.NextCursor == "" {
			break
		}
		q.Cursor = p.NextCursor
	}
	if len(got) != len(want) {
		t.Fatalf("walked %d items, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].TransactionID != want[i].TransactionID {
			t.Fatalf("item %d: %d, want %d", i, got[i].TransactionID, want[i].TransactionID)
		}
	}
}

func TestFilters(t *testing.T) {
	e := setup(t)
	e.move(t, e.a, e.b, 100_000, "")
	e.move(t, e.b, e.a, 30_000, "")

	if n := len(e.all(t, Query{Sub: "sub-a", Direction: "out"})); n != 1 {
		t.Errorf("out = %d, want 1", n)
	}
	if n := len(e.all(t, Query{Sub: "sub-a", Direction: "in"})); n != 2 {
		t.Errorf("in = %d, want 2 (opening + transfer)", n)
	}
	if n := len(e.all(t, Query{Sub: "sub-a", Category: "food"})); n != 0 {
		t.Errorf("food = %d, want 0", n)
	}
	future := time.Now().Add(time.Hour)
	if n := len(e.all(t, Query{Sub: "sub-a", From: future})); n != 0 {
		t.Errorf("from future = %d, want 0", n)
	}
	if n := len(e.all(t, Query{Sub: "sub-a", To: future})); n != 3 {
		t.Errorf("to future = %d, want 3", n)
	}
}

func TestOnlyOwnHistory(t *testing.T) {
	e := setup(t)
	c, _ := accounts.Service{Pool: e.pool}.Me(context.Background(), "sub-c", "Le Van Cuong", "")
	e.move(t, e.a, c, 50_000, "secret")

	for _, it := range e.all(t, Query{Sub: "sub-b"}) {
		if it.Note == "secret" {
			t.Fatal("B sees a transfer between A and C")
		}
	}
}

func TestInvalid(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	now := time.Now()
	for name, q := range map[string]Query{
		"limit too big":  {Sub: "sub-a", Limit: MaxLimit + 1},
		"bad direction":  {Sub: "sub-a", Direction: "sideways"},
		"from after to":  {Sub: "sub-a", From: now, To: now.Add(-time.Hour)},
		"garbage cursor": {Sub: "sub-a", Cursor: "!!"},
		"forged cursor":  {Sub: "sub-a", Cursor: encodeCursor(0)},
	} {
		if _, err := e.svc.List(ctx, q); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := e.svc.List(ctx, Query{Sub: "nobody"}); !errors.Is(err, ErrNoAccount) {
		t.Errorf("no account: err = %v", err)
	}
}
