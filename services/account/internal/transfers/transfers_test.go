package transfers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ducnghiaaa/minibank/internal/platform/dbtest"
	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
	"github.com/ducnghiaaa/minibank/services/account/internal/db"
	"github.com/ducnghiaaa/minibank/services/account/internal/ledger"
	"github.com/ducnghiaaa/minibank/services/account/internal/txntoken"
)

// fakeTokens treats the raw token as "jti" and approves whatever the test registered for it.
type fakeTokens struct {
	mu     sync.Mutex
	claims map[string]txntoken.Claims
}

func (f *fakeTokens) Verify(raw string) (txntoken.Claims, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.claims[raw]
	if !ok {
		return txntoken.Claims{}, errors.New("bad signature")
	}
	return c, nil
}

var jtiSeq atomic.Int64

// approve issues a token for exactly this request, as the security service would.
func (f *fakeTokens) approve(r Request) Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	jti := fmt.Sprintf("jti-%d", jtiSeq.Add(1))
	f.claims[jti] = txntoken.Claims{
		Sub: r.Sub, JTI: jti, ExpiresAt: time.Now().Add(time.Minute), Type: "TRANSFER",
		ToAccount: r.ToAccount, Amount: r.Amount, IdempotencyKey: r.IdempotencyKey,
	}
	r.Token = jti
	return r
}

type env struct {
	svc    Service
	tokens *fakeTokens
	a, b   accounts.Account
}

func setup(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	acc := accounts.Service{Pool: pool}
	a, err := acc.Me(ctx, "sub-a", "An")
	if err != nil {
		t.Fatal(err)
	}
	b, err := acc.Me(ctx, "sub-b", "Binh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.CheckInvariants(ctx, pool); err != nil {
			t.Error(err)
		}
	})
	tokens := &fakeTokens{claims: map[string]txntoken.Claims{}}
	return env{svc: Service{Pool: pool, Tokens: tokens}, tokens: tokens, a: a, b: b}
}

func (e env) balance(t *testing.T, number string) int64 {
	var b int64
	err := e.svc.Pool.QueryRow(context.Background(),
		`SELECT b.balance FROM balances b JOIN accounts a ON a.id = b.account_id WHERE a.number = $1`, number).Scan(&b)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e env) req(key string, amount int64) Request {
	return e.tokens.approve(Request{Sub: "sub-a", ToAccount: e.b.Number, Amount: amount, Note: "an trua", IdempotencyKey: key})
}

func TestTransfer(t *testing.T) {
	e := setup(t)
	res, err := e.svc.Transfer(context.Background(), e.req("k1", 500_000))
	if err != nil {
		t.Fatal(err)
	}
	if res.BalanceAfter != 9_500_000 || res.ToAccount != e.b.Number || res.Replayed {
		t.Errorf("result = %+v", res)
	}
	if a, b := e.balance(t, e.a.Number), e.balance(t, e.b.Number); a != 9_500_000 || b != 10_500_000 {
		t.Errorf("balances a=%d b=%d", a, b)
	}
}

func TestRetrySameKeyReturnsSameResult(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	r := e.req("k1", 500_000)
	first, err := e.svc.Transfer(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	// same request, same (now used) token: a slow retry still gets the old result
	again, err := e.svc.Transfer(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if again.TransactionID != first.TransactionID || !again.Replayed || again.BalanceAfter != first.BalanceAfter {
		t.Errorf("retry = %+v, first = %+v", again, first)
	}
	if a := e.balance(t, e.a.Number); a != 9_500_000 {
		t.Errorf("money moved twice: a=%d", a)
	}
}

func TestSameKeyDifferentRequest(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if _, err := e.svc.Transfer(ctx, e.req("k1", 500_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Transfer(ctx, e.req("k1", 600_000)); !errors.Is(err, ErrIdempotencyReused) {
		t.Errorf("err = %v, want ErrIdempotencyReused", err)
	}
}

func TestTokenChecks(t *testing.T) {
	ctx := context.Background()
	e := setup(t)

	used := e.req("k1", 500_000)
	if _, err := e.svc.Transfer(ctx, used); err != nil {
		t.Fatal(err)
	}
	replay := used
	replay.IdempotencyKey = "k2" // new key, old token
	tampered := e.req("k3", 500_000)
	tampered.Amount = 5_000_000 // token approved 500k
	otherUser := e.tokens.approve(Request{Sub: "sub-b", ToAccount: e.b.Number, Amount: 500_000, IdempotencyKey: "k4"})
	otherUser.Sub = "sub-a"
	forged := e.req("k5", 500_000)
	forged.Token = "not-signed"

	cases := map[string]struct {
		r    Request
		want error
	}{
		"old token, new key":   {replay, ErrTokenInvalid}, // the token approved k1, not k2
		"amount changed":       {tampered, ErrTokenInvalid},
		"token for other user": {otherUser, ErrTokenInvalid},
		"bad signature":        {forged, ErrTokenInvalid},
	}
	for name, c := range cases {
		if _, err := e.svc.Transfer(ctx, c.r); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}

}

func TestTokenUsedOnce(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	r := e.req("k1", 500_000)
	c := e.tokens.claims[r.Token]
	// a second token with the same jti, approving a different key: jti check must reject it
	c2 := c
	c2.IdempotencyKey = "k2"
	e.tokens.claims["same-jti"] = c2
	if _, err := e.svc.Transfer(ctx, r); err != nil {
		t.Fatal(err)
	}
	r2 := r
	r2.IdempotencyKey, r2.Token = "k2", "same-jti"
	if _, err := e.svc.Transfer(ctx, r2); !errors.Is(err, ErrTokenAlreadyUsed) {
		t.Errorf("err = %v, want ErrTokenAlreadyUsed", err)
	}
}

func TestRules(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	var merchant string
	e.svc.Pool.QueryRow(ctx, `SELECT number FROM accounts WHERE type = 'MERCHANT' LIMIT 1`).Scan(&merchant)

	to := func(number string, amount int64, note, key string) Request {
		return e.tokens.approve(Request{Sub: "sub-a", ToAccount: number, Amount: amount, Note: note, IdempotencyKey: key})
	}
	cases := map[string]struct {
		r    Request
		want error
	}{
		"below minimum":           {to(e.b.Number, 999, "", "a"), ErrInvalid},
		"above maximum":           {to(e.b.Number, 50_000_001, "", "b"), ErrInvalid},
		"note too long":           {to(e.b.Number, 1_000, strings.Repeat("x", 141), "c"), ErrInvalid},
		"bad check digit":         {to("7020113548", 1_000, "", "d"), ErrInvalid},
		"no key":                  {to(e.b.Number, 1_000, "", ""), ErrInvalid},
		"unknown recipient":       {to("7020113549", 1_000, "", "e"), ErrRecipientNotFound},
		"merchant via /transfers": {to(merchant, 1_000, "", "f"), ErrRecipientNotFound},
		"to yourself":             {to(e.a.Number, 1_000, "", "g"), ErrSelfTransfer},
		"more than balance":       {to(e.b.Number, 20_000_000, "", "h"), ErrInsufficientFunds},
		"no account":              {e.tokens.approve(Request{Sub: "nobody", ToAccount: e.b.Number, Amount: 1_000, IdempotencyKey: "i"}), ErrNoAccount},
	}
	for name, c := range cases {
		if _, err := e.svc.Transfer(ctx, c.r); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if a := e.balance(t, e.a.Number); a != 10_000_000 {
		t.Errorf("a rejected transfer moved money: a=%d", a)
	}
}

func TestDailyLimit(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	// give A enough to hit the 100M daily limit before running out
	tx, _ := e.svc.Pool.Begin(ctx)
	var bank int64
	tx.QueryRow(ctx, `SELECT id FROM accounts WHERE type = 'SYSTEM'`).Scan(&bank)
	if _, err := ledger.Post(ctx, tx, ledger.Entry{Type: "OPENING", From: bank, To: e.a.ID, Amount: 200_000_000,
		Initiator: e.a.ID, IdempotencyKey: "top-up", RequestHash: "top-up"}); err != nil {
		t.Fatal(err)
	}
	tx.Commit(ctx)

	for i, amount := range []int64{50_000_000, 50_000_000} {
		if _, err := e.svc.Transfer(ctx, e.req(fmt.Sprint("ok-", i), amount)); err != nil {
			t.Fatalf("transfer %d: %v", i, err)
		}
	}
	if _, err := e.svc.Transfer(ctx, e.req("over", 1_000)); !errors.Is(err, ErrDailyLimit) {
		t.Errorf("err = %v, want ErrDailyLimit", err)
	}
}

func TestParallelSameKey(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	r := e.req("k1", 500_000)

	var wg sync.WaitGroup
	ids := make([]int64, 8)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := e.svc.Transfer(ctx, r)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = res.TransactionID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("different transactions for one key: %v", ids)
		}
	}
	if a := e.balance(t, e.a.Number); a != 9_500_000 {
		t.Errorf("money moved more than once: a=%d", a)
	}
}

func TestOppositeTransfersDoNotDeadlock(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	var wg sync.WaitGroup
	for i := range 20 {
		for _, dir := range []struct{ sub, to string }{{"sub-a", e.b.Number}, {"sub-b", e.a.Number}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := e.tokens.approve(Request{Sub: dir.sub, ToAccount: dir.to, Amount: 1_000, IdempotencyKey: fmt.Sprint(dir.sub, i)})
				if _, err := e.svc.Transfer(ctx, r); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()
	// 20 each way: everyone is back where they started
	if a, b := e.balance(t, e.a.Number), e.balance(t, e.b.Number); a != 10_000_000 || b != 10_000_000 {
		t.Errorf("balances a=%d b=%d", a, b)
	}
}
