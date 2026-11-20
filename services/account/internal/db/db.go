// Package db opens the Postgres pool and runs migrations for the account service.
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/ducnghiaaa/minibank/services/account/migrations"
)

// Open returns a connection pool and checks that the database answers.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
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
