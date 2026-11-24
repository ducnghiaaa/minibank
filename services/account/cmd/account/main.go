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
	"time"

	"github.com/MicahParks/keyfunc/v3"

	"github.com/ducnghiaaa/minibank/internal/platform/auth"
	"github.com/ducnghiaaa/minibank/internal/platform/httpserver"
	"github.com/ducnghiaaa/minibank/internal/platform/logging"
	"github.com/ducnghiaaa/minibank/services/account/internal/accounts"
	"github.com/ducnghiaaa/minibank/services/account/internal/api"
	"github.com/ducnghiaaa/minibank/services/account/internal/db"
	"github.com/ducnghiaaa/minibank/services/account/internal/history"
	"github.com/ducnghiaaa/minibank/services/account/internal/transfers"
	"github.com/ducnghiaaa/minibank/services/account/internal/txntoken"
)

func main() {
	log := logging.New("account")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// local: password in DATABASE_URL; AWS: DB_IAM_AUTH=true, no password anywhere
	pool, err := db.Open(ctx, log, db.Config{
		URL:         mustEnv(log, "DATABASE_URL"),
		IAMAuth:     os.Getenv("DB_IAM_AUTH") == "true",
		Region:      getenv("AWS_REGION", "ap-southeast-1"),
		IAMEndpoint: os.Getenv("DB_IAM_ENDPOINT"),
		WakeTimeout: 60 * time.Second,
	})
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

	// Cognito in AWS, devtoken locally; keys are fetched and refreshed from the JWKS URL
	jwks, err := keyfunc.NewDefaultCtx(ctx, []string{mustEnv(log, "AUTH_JWKS_URL")})
	if err != nil {
		log.Error("jwks", "err", err)
		os.Exit(1)
	}
	verifier := auth.Verifier{Issuer: mustEnv(log, "AUTH_ISSUER"), Keyfunc: jwks.Keyfunc}

	// transaction tokens come from the security service (devtoken locally), with their own keys
	txnJWKS, err := keyfunc.NewDefaultCtx(ctx, []string{mustEnv(log, "TXN_JWKS_URL")})
	if err != nil {
		log.Error("txn jwks", "err", err)
		os.Exit(1)
	}
	txnVerifier := txntoken.Verifier{Issuer: mustEnv(log, "TXN_ISSUER"), Keyfunc: txnJWKS.Keyfunc}

	// Cognito: name comes from userInfo; devtoken puts it in the token, so no URL locally
	var names accounts.NameSource
	if u := os.Getenv("AUTH_USERINFO_URL"); u != "" {
		names = auth.UserInfo{URL: u}
	}

	router := api.NewRouter(api.Deps{
		DB:        pool,
		Accounts:  accounts.Service{Pool: pool, Names: names},
		Transfers: transfers.Service{Pool: pool, Tokens: txnVerifier},
		History:   history.Service{Pool: pool},
		Auth:      verifier.Middleware,
		Log:       log,
	})
	addr := ":" + getenv("PORT", "8080")
	if err := httpserver.Run(ctx, log, addr, router); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func mustEnv(log interface{ Error(string, ...any) }, key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Error(key + " is not set")
		os.Exit(1)
	}
	return v
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
