package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserInfoName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"sub":"s1","name":"Nguyễn Văn An","email":"an@example.com"}`))
	}))
	defer srv.Close()
	u := UserInfo{URL: srv.URL}

	if name, err := u.Name(context.Background(), "good"); err != nil || name != "Nguyễn Văn An" {
		t.Errorf("got %q, %v", name, err)
	}
	if _, err := u.Name(context.Background(), "bad"); err == nil {
		t.Error("401 from userInfo must be an error")
	}
}
