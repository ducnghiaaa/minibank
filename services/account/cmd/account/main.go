// Command account runs the account service: accounts, ledger, transfers, history.
//
//	account          serve HTTP
//	account migrate  apply database migrations, then exit
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ducnghiaaa/minibank/internal/platform/httpserver"
	"github.com/ducnghiaaa/minibank/internal/platform/logging"
	"github.com/ducnghiaaa/minibank/services/account/internal/api"
	"github.com/ducnghiaaa/minibank/services/account/internal/db"
)

func main() {
	log := logging.New("account")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Error("DATABASE_URL is not set")
		os.Exit(1)
	}
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := db.Migrate(ctx, pool); err != nil {
			log.Error("migrate", "err", err)
			os.Exit(1)
		}
		log.Info("migrations applied")
		return
	}

	addr := ":" + getenv("PORT", "8080")
	if err := httpserver.Run(ctx, log, addr, api.NewRouter(pool)); err != nil {
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
