package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ducnghiaaa/minibank/internal/platform/auth"
	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
	"github.com/ducnghiaaa/minibank/services/account/internal/history"
	"github.com/ducnghiaaa/minibank/services/account/internal/transfers"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

type fakeAccounts struct {
	gotSub    string
	lookupErr error
}

func (f *fakeAccounts) Lookup(_ context.Context, number string) (accounts.Recipient, error) {
	return accounts.Recipient{Number: number, MaskedName: "NGUYEN V. A"}, f.lookupErr
}

type fakeHistory struct {
	got history.Query
	err error
}

func (f *fakeHistory) List(_ context.Context, q history.Query) (history.Page, error) {
	f.got = q
	return history.Page{Items: []history.Item{}}, f.err
}

func (f *fakeAccounts) Me(_ context.Context, sub, _ string) (accounts.Account, error) {
	f.gotSub = sub
	return accounts.Account{Number: "7020113549", DisplayName: "NGUYEN VAN AN", Balance: 10_000_000}, nil
}

// fakeAuth lets requests with "Bearer ok" through as sub-1, like the real middleware would.
func fakeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), auth.Claims{Sub: "sub-1"})))
	})
}

type fakeTransfers struct {
	got transfers.Request
	res transfers.Result
	err error
}

func (f *fakeTransfers) Transfer(_ context.Context, r transfers.Request) (transfers.Result, error) {
	f.got = r
	return f.res, f.err
}

func routerWith(dbErr error, acc *fakeAccounts, tr *fakeTransfers) http.Handler {
	return routerFull(dbErr, acc, tr, &fakeHistory{})
}

func routerFull(dbErr error, acc *fakeAccounts, tr *fakeTransfers, h *fakeHistory) http.Handler {
	return NewRouter(Deps{DB: fakeDB{dbErr}, Accounts: acc, Transfers: tr, History: h, Auth: fakeAuth,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func router(dbErr error, acc *fakeAccounts) http.Handler {
	return routerWith(dbErr, acc, &fakeTransfers{})
}

func do(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	// liveness stays OK even when the DB is down
	if code := do(router(errors.New("down"), &fakeAccounts{}), "/healthz", "").Code; code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", code)
	}
}

func TestReadyz(t *testing.T) {
	if code := do(router(nil, &fakeAccounts{}), "/readyz", "").Code; code != http.StatusOK {
		t.Errorf("DB up: GET /readyz = %d, want 200", code)
	}
	if code := do(router(errors.New("down"), &fakeAccounts{}), "/readyz", "").Code; code != http.StatusServiceUnavailable {
		t.Errorf("DB down: GET /readyz = %d, want 503", code)
	}
}

func TestMe(t *testing.T) {
	acc := &fakeAccounts{}
	h := router(nil, acc)

	if code := do(h, "/accounts/me", "").Code; code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", code)
	}
	rec := do(h, "/accounts/me", "ok")
	if rec.Code != http.StatusOK || acc.gotSub != "sub-1" {
		t.Fatalf("got %d, sub %q", rec.Code, acc.gotSub)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"balance":10000000`) || strings.Contains(body, `"id"`) {
		t.Errorf("body = %s", body)
	}
}

func TestUnknownPath(t *testing.T) {
	if code := do(router(nil, &fakeAccounts{}), "/nope", "").Code; code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", code)
	}
}

func postTransfer(h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer ok")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTransfersPassesHeadersAndBody(t *testing.T) {
	tr := &fakeTransfers{res: transfers.Result{TransactionID: 7, Replayed: true}}
	rec := postTransfer(routerWith(nil, &fakeAccounts{}, tr), `{"to_account":"7020113549","amount":500000,"note":"hi"}`,
		map[string]string{"Idempotency-Key": "k1", "X-Transaction-Token": "tok"})
	want := transfers.Request{Sub: "sub-1", ToAccount: "7020113549", Amount: 500000, Note: "hi", IdempotencyKey: "k1", Token: "tok"}
	if rec.Code != http.StatusOK || tr.got != want {
		t.Fatalf("code %d, request %+v", rec.Code, tr.got)
	}
	if rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("replayed result not marked")
	}
}

func TestTransfersErrorCodes(t *testing.T) {
	cases := map[error]int{
		transfers.ErrInvalid:           http.StatusBadRequest,
		transfers.ErrTokenInvalid:      http.StatusForbidden,
		transfers.ErrTokenAlreadyUsed:  http.StatusForbidden,
		transfers.ErrNoAccount:         http.StatusNotFound,
		transfers.ErrIdempotencyReused: http.StatusConflict,
		transfers.ErrRecipientNotFound: http.StatusUnprocessableEntity,
		transfers.ErrSelfTransfer:      http.StatusUnprocessableEntity,
		transfers.ErrDailyLimit:        http.StatusUnprocessableEntity,
		transfers.ErrInsufficientFunds: http.StatusUnprocessableEntity,
		errors.New("db is on fire"):    http.StatusInternalServerError,
	}
	for err, want := range cases {
		rec := postTransfer(routerWith(nil, &fakeAccounts{}, &fakeTransfers{err: err}), `{}`, nil)
		if rec.Code != want {
			t.Errorf("%v: %d, want %d", err, rec.Code, want)
		}
		if want == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "fire") {
			t.Error("internal error details leaked to the client")
		}
	}
	if rec := postTransfer(router(nil, &fakeAccounts{}), `not json`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON: %d, want 400", rec.Code)
	}
}

func TestLookup(t *testing.T) {
	h := router(nil, &fakeAccounts{})
	if rec := do(h, "/accounts/lookup?number=7020113549", "ok"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "NGUYEN V. A") {
		t.Errorf("valid: %d %s", rec.Code, rec.Body)
	}
	for _, n := range []string{"", "123", "7020113548", "abcdefghij"} {
		if code := do(h, "/accounts/lookup?number="+n, "ok").Code; code != http.StatusBadRequest {
			t.Errorf("number %q: %d, want 400", n, code)
		}
	}
	missing := router(nil, &fakeAccounts{lookupErr: accounts.ErrNotFound})
	if code := do(missing, "/accounts/lookup?number=7020113549", "ok").Code; code != http.StatusNotFound {
		t.Errorf("not found: %d, want 404", code)
	}
	if code := do(h, "/accounts/lookup?number=7020113549", "").Code; code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", code)
	}
}

func TestTransactionsQuery(t *testing.T) {
	hist := &fakeHistory{}
	h := routerFull(nil, &fakeAccounts{}, &fakeTransfers{}, hist)
	rec := do(h, "/transactions?limit=5&direction=out&category=food&cursor=abc&from=2026-11-20T00:00:00%2B07:00", "ok")
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	if hist.got.Sub != "sub-1" || hist.got.Limit != 5 || hist.got.Direction != "out" || hist.got.Category != "food" ||
		hist.got.Cursor != "abc" || hist.got.From.IsZero() || !hist.got.To.IsZero() {
		t.Errorf("query = %+v", hist.got)
	}
	for _, bad := range []string{"limit=x", "from=yesterday"} {
		if code := do(h, "/transactions?"+bad, "ok").Code; code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, code)
		}
	}
	invalid := routerFull(nil, &fakeAccounts{}, &fakeTransfers{}, &fakeHistory{err: history.ErrInvalid})
	if code := do(invalid, "/transactions", "ok").Code; code != http.StatusBadRequest {
		t.Errorf("service ErrInvalid: %d, want 400", code)
	}
}
