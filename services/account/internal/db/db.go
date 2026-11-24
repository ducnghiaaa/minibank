// Package db opens the Postgres pool and runs migrations for the account service.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/ducnghiaaa/minibank/services/account/migrations"
)

// Config says how to reach Postgres.
type Config struct {
	URL string // postgres://user[:password]@host:port/db?sslmode=...

	// IAMAuth: no password; each new connection gets a fresh 15-minute IAM token (app spec §10).
	// The pod's AWS credentials come from Pod Identity. Aurora requires TLS for IAM auth.
	IAMAuth bool
	Region  string
	// IAMEndpoint is the Aurora host:port the token is signed for, when URL points elsewhere
	// (e.g. localhost through an SSM tunnel). Empty = the host:port in URL.
	IAMEndpoint string

	// WakeTimeout: how long to wait for the first answer. Aurora at 0 ACU takes ~15s to resume.
	WakeTimeout time.Duration
}

// Open returns a connection pool once the database answers, waiting up to cfg.WakeTimeout.
func Open(ctx context.Context, log *slog.Logger, cfg Config) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.IAMAuth {
		if err := useIAMAuth(ctx, pcfg, cfg); err != nil {
			return nil, err
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := waitFor(ctx, log, pool.Ping, cfg.WakeTimeout); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func useIAMAuth(ctx context.Context, pcfg *pgxpool.Config, cfg Config) error {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	endpoint := cfg.IAMEndpoint
	if endpoint == "" {
		endpoint = net.JoinHostPort(pcfg.ConnConfig.Host, strconv.Itoa(int(pcfg.ConnConfig.Port)))
	}
	// tokens live 15 minutes, so build one per new connection, never once at startup
	pcfg.BeforeConnect = func(ctx context.Context, cc *pgx.ConnConfig) error {
		token, err := auth.BuildAuthToken(ctx, endpoint, cfg.Region, cc.User, awsCfg.Credentials)
		if err != nil {
			return fmt.Errorf("iam auth token: %w", err)
		}
		cc.Password = token
		return nil
	}
	return nil
}

// waitFor retries ping with backoff (1s, 2s, 4s, capped at 5s) until it succeeds or timeout passes.
func waitFor(ctx context.Context, log *slog.Logger, ping func(context.Context) error, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := time.Second
	for {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := ping(pctx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().Add(delay).After(deadline) {
			return err
		}
		log.Info("database not ready, retrying", "in", delay.String(), "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}

// Migrate applies all pending migrations. It runs as its own step before deploy, never at startup.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return withGoose(pool, func(sqlDB *sql.DB) error {
		return goose.UpContext(ctx, sqlDB, ".")
	})
}

// Reset rolls back every migration. Tests only.
func Reset(ctx context.Context, pool *pgxpool.Pool) error {
	return withGoose(pool, func(sqlDB *sql.DB) error {
		return goose.ResetContext(ctx, sqlDB, ".")
	})
}

// goose works on database/sql, so wrap the pgx pool for the duration of the call.
func withGoose(pool *pgxpool.Pool, fn func(*sql.DB) error) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	return fn(sqlDB)
}
