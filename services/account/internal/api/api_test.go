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
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

type fakeAccounts struct{ gotSub string }

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

func router(dbErr error, acc *fakeAccounts) http.Handler {
	return NewRouter(Deps{DB: fakeDB{dbErr}, Accounts: acc, Auth: fakeAuth, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
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
