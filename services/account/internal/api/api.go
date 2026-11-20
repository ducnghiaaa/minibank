// Package api holds the HTTP handlers of the account service.
package api

import "net/http"

// NewRouter returns the account service routes.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	// liveness: the process is up
	mux.HandleFunc("GET /healthz", ok)
	// readiness: will also check the database (PR 2)
	mux.HandleFunc("GET /readyz", ok)
	return mux
}

func ok(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}
