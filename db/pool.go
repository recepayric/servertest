package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is a global connection pool reused by handlers.
var Pool *pgxpool.Pool

// Init initializes the global connection pool.
func Init(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Reasonable pool defaults for small Render free-tier apps
	cfg.MaxConns = 5
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("ping: %w", err)
	}

	Pool = pool
	log.Println("✅ Connected to Postgres")
	return nil
}

// Close closes the global pool. Call this on server shutdown.
func Close() {
	if Pool != nil {
		Pool.Close()
	}
}

