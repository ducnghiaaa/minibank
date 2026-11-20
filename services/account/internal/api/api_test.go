package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func get(h http.Handler, path string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

func TestHealthz(t *testing.T) {
	// liveness stays OK even when the DB is down
	if code := get(NewRouter(fakeDB{err: errors.New("down")}), "/healthz"); code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", code)
	}
}

func TestReadyz(t *testing.T) {
	if code := get(NewRouter(fakeDB{}), "/readyz"); code != http.StatusOK {
		t.Errorf("DB up: GET /readyz = %d, want 200", code)
	}
	if code := get(NewRouter(fakeDB{err: errors.New("down")}), "/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("DB down: GET /readyz = %d, want 503", code)
	}
}

func TestUnknownPath(t *testing.T) {
	if code := get(NewRouter(fakeDB{}), "/nope"); code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", code)
	}
}
