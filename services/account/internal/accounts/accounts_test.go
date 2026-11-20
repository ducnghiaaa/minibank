package accounts

import (
	"context"
	"sync"
	"testing"

	"github.com/ducnghiaaa/minibank/internal/platform/dbtest"
	"github.com/ducnghiaaa/minibank/services/account/internal/db"
	"github.com/ducnghiaaa/minibank/services/account/internal/ledger"
)

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"Nguyễn Văn An":     "NGUYEN VAN AN",
		"  trần   thị  bé ": "TRAN THI BE",
		"Đặng Đức":          "DANG DUC",
		"":                  "KHACH HANG",
	}
	for in, want := range cases {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNumbers(t *testing.T) {
	for range 1000 {
		n, err := newNumber()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidNumber(n) || n[0] == '0' || n[0] == '9' {
			t.Fatalf("bad number %q", n)
		}
	}
	// the seed numbers and the prototype sample are valid; one changed digit is caught
	for _, n := range []string{"0000000000", "9000000019", "7020113549"} {
		if !ValidNumber(n) {
			t.Errorf("%s should be valid", n)
		}
	}
	if ValidNumber("7020113548") {
		t.Error("mistyped number accepted")
	}
}

func setup(t *testing.T) Service {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.New(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.CheckInvariants(ctx, pool); err != nil {
			t.Error(err)
		}
	})
	return Service{Pool: pool}
}

func TestMeOpensOnceWithGift(t *testing.T) {
	ctx := context.Background()
	s := setup(t)

	a, err := s.Me(ctx, "sub-1", "Nguyễn Văn An")
	if err != nil {
		t.Fatal(err)
	}
	if a.Balance != OpeningGift || a.DisplayName != "NGUYEN VAN AN" || !ValidNumber(a.Number) {
		t.Errorf("first call: %+v", a)
	}
	again, err := s.Me(ctx, "sub-1", "Other Name")
	if err != nil {
		t.Fatal(err)
	}
	if again != a {
		t.Errorf("second call changed the account: %+v vs %+v", again, a)
	}
}

func TestMeInParallel(t *testing.T) {
	ctx := context.Background()
	s := setup(t)

	var wg sync.WaitGroup
	numbers := make([]string, 10)
	for i := range numbers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := s.Me(ctx, "sub-race", "An")
			if err != nil {
				t.Error(err)
				return
			}
			numbers[i] = a.Number
		}()
	}
	wg.Wait()

	for _, n := range numbers {
		if n != numbers[0] {
			t.Fatalf("parallel calls returned different accounts: %v", numbers)
		}
	}
	var accounts, openings int
	s.Pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_sub = 'sub-race'`).Scan(&accounts)
	s.Pool.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE type = 'OPENING'`).Scan(&openings)
	if accounts != 1 || openings != 1 {
		t.Errorf("accounts=%d openings=%d, want 1 and 1", accounts, openings)
	}
}
