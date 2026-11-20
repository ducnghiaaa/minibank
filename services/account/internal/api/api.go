// Package api holds the HTTP handlers of the account service.
package api

import (
	"context"
	"net/http"
	"time"
)

// Pinger reports whether the database answers.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewRouter returns the account service routes.
func NewRouter(db Pinger) http.Handler {
	mux := http.NewServeMux()
	// liveness: the process is up; never checks the DB, or a DB outage would restart every pod
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { text(w, http.StatusOK, "ok") })
	// readiness: no traffic until the DB answers (Aurora can take ~15s to wake from 0 ACU)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			text(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		text(w, http.StatusOK, "ok")
	})
	return mux
}

func text(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(msg + "\n"))
}
