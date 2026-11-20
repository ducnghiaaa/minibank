// Command account runs the account service: accounts, ledger, transfers, history.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ducnghiaaa/minibank/internal/platform/httpserver"
	"github.com/ducnghiaaa/minibank/internal/platform/logging"
	"github.com/ducnghiaaa/minibank/services/account/internal/api"
)

func main() {
	log := logging.New("account")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := ":" + getenv("PORT", "8080")
	if err := httpserver.Run(ctx, log, addr, api.NewRouter()); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
