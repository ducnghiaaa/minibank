// Package api holds the HTTP handlers of the account service.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ducnghiaaa/minibank/internal/platform/auth"
	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
)

// Pinger reports whether the database answers.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Accounts opens and reads the caller's account.
type Accounts interface {
	Me(ctx context.Context, sub, name string) (accounts.Account, error)
}

// Deps are what the handlers need.
type Deps struct {
	DB       Pinger
	Accounts Accounts
	Auth     func(http.Handler) http.Handler
	Log      *slog.Logger
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
		a, err := d.Accounts.Me(r.Context(), c.Sub, c.Name)
		if err != nil {
			d.Log.Error("accounts/me", "err", err)
			text(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a)
	})))
	return mux
}

func text(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(msg + "\n"))
}
