package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestWaitForRetriesUntilAwake(t *testing.T) {
	calls := 0
	ping := func(context.Context) error {
		calls++
		if calls < 3 { // like Aurora resuming from 0 ACU
			return errors.New("connection refused")
		}
		return nil
	}
	if err := waitFor(context.Background(), quiet, ping, 10*time.Second); err != nil || calls != 3 {
		t.Errorf("err=%v calls=%d, want nil and 3", err, calls)
	}
}

func TestWaitForGivesUp(t *testing.T) {
	start := time.Now()
	err := waitFor(context.Background(), quiet, func(context.Context) error { return errors.New("down") }, 2*time.Second)
	if err == nil || time.Since(start) > 4*time.Second {
		t.Errorf("err=%v after %v, want an error within the timeout", err, time.Since(start))
	}
}
