// Package api holds the HTTP handlers of the account service.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ducnghiaaa/minibank/internal/platform/auth"
	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
	"github.com/ducnghiaaa/minibank/services/account/internal/history"
	"github.com/ducnghiaaa/minibank/services/account/internal/transfers"
)

// Pinger reports whether the database answers.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Accounts opens and reads accounts.
type Accounts interface {
	Me(ctx context.Context, sub, name, token string) (accounts.Account, error)
	Lookup(ctx context.Context, number string) (accounts.Recipient, error)
}

// History lists the caller's transactions.
type History interface {
	List(ctx context.Context, q history.Query) (history.Page, error)
}

// Transfers moves money between customers.
type Transfers interface {
	Transfer(ctx context.Context, r transfers.Request) (transfers.Result, error)
}

// Deps are what the handlers need.
type Deps struct {
	DB        Pinger
	Accounts  Accounts
	Transfers Transfers
	History   History
	Auth      func(http.Handler) http.Handler
	Log       *slog.Logger
}

// NewRouter returns the account service routes.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	// liveness: the process is up; never checks the DB, or a DB outage would restart every pod
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { text(w, http.StatusOK, "ok") })
	// readiness: no traffic until the DB answers (Aurora can take ~15s to wake from 0 ACU)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			text(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		text(w, http.StatusOK, "ok")
	})

	mux.Handle("GET /accounts/me", d.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := auth.FromContext(r.Context())
		a, err := d.Accounts.Me(r.Context(), c.Sub, c.Name, c.Token)
		if err != nil {
			d.Log.Error("accounts/me", "err", err)
			text(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, a)
	})))

	// rate-limited per caller at Envoy (BackendTrafficPolicy), so it cannot be used to scan names
	mux.Handle("GET /accounts/lookup", d.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		number := r.URL.Query().Get("number")
		if !accounts.ValidNumber(number) {
			writeError(w, http.StatusBadRequest, "invalid_request", "number must be 10 digits with a valid check digit")
			return
		}
		rc, err := d.Accounts.Lookup(r.Context(), number)
		if errors.Is(err, accounts.ErrNotFound) {
			writeError(w, http.StatusNotFound, "recipient_not_found", "no customer account has this number")
			return
		}
		if err != nil {
			d.Log.Error("accounts/lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		writeJSON(w, http.StatusOK, rc)
	})))

	mux.Handle("GET /transactions", d.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := auth.FromContext(r.Context())
		q, err := historyQuery(r, c.Sub)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		page, err := d.History.List(r.Context(), q)
		switch {
		case errors.Is(err, history.ErrInvalid):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, history.ErrNoAccount):
			writeError(w, http.StatusNotFound, "no_account", err.Error())
		case err != nil:
			d.Log.Error("transactions", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "internal error")
		default:
			writeJSON(w, http.StatusOK, page)
		}
	})))

	mux.Handle("POST /transfers", d.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ToAccount string `json:"to_account"`
			Amount    int64  `json:"amount"`
			Note      string `json:"note"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "body must be JSON: to_account, amount, note")
			return
		}
		c, _ := auth.FromContext(r.Context())
		res, err := d.Transfers.Transfer(r.Context(), transfers.Request{
			Sub: c.Sub, ToAccount: body.ToAccount, Amount: body.Amount, Note: body.Note,
			IdempotencyKey: r.Header.Get("Idempotency-Key"),
			Token:          r.Header.Get("X-Transaction-Token"),
		})
		if err != nil {
			code, slug := transferError(err)
			if code == http.StatusInternalServerError {
				d.Log.Error("transfers", "err", err)
				writeError(w, code, slug, "internal error")
				return
			}
			writeError(w, code, slug, err.Error())
			return
		}
		if res.Replayed {
			w.Header().Set("Idempotent-Replayed", "true")
		}
		writeJSON(w, http.StatusOK, res)
	})))
	return mux
}

// historyQuery reads ?cursor, limit, from, to (RFC 3339), direction, category.
func historyQuery(r *http.Request, sub string) (history.Query, error) {
	v := r.URL.Query()
	q := history.Query{Sub: sub, Cursor: v.Get("cursor"), Direction: v.Get("direction"), Category: v.Get("category")}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return q, errors.New("limit must be a number")
		}
		q.Limit = n
	}
	for name, dst := range map[string]*time.Time{"from": &q.From, "to": &q.To} {
		if s := v.Get(name); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return q, errors.New(name + " must be an RFC 3339 time, e.g. 2026-11-20T00:00:00+07:00")
			}
			*dst = t
		}
	}
	return q, nil
}

// transferError maps a transfer error to an HTTP status and a stable code the frontend can switch on.
func transferError(err error) (int, string) {
	switch {
	case errors.Is(err, transfers.ErrInvalid):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, transfers.ErrTokenInvalid):
		return http.StatusForbidden, "transaction_token_invalid"
	case errors.Is(err, transfers.ErrTokenAlreadyUsed):
		return http.StatusForbidden, "transaction_token_used"
	case errors.Is(err, transfers.ErrNoAccount):
		return http.StatusNotFound, "no_account"
	case errors.Is(err, transfers.ErrIdempotencyReused):
		return http.StatusConflict, "idempotency_key_reused"
	case errors.Is(err, transfers.ErrRecipientNotFound):
		return http.StatusUnprocessableEntity, "recipient_not_found"
	case errors.Is(err, transfers.ErrSelfTransfer):
		return http.StatusUnprocessableEntity, "self_transfer"
	case errors.Is(err, transfers.ErrDailyLimit):
		return http.StatusUnprocessableEntity, "daily_limit_exceeded"
	case errors.Is(err, transfers.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, "insufficient_funds"
	}
	return http.StatusInternalServerError, "internal"
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, slug, msg string) {
	writeJSON(w, code, map[string]string{"error": slug, "message": msg})
}

func text(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(msg + "\n"))
}
