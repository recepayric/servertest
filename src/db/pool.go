package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Init(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

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

	// Auto-migrate
	_, _ = pool.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT`)
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS groups (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL, owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE INDEX IF NOT EXISTS idx_groups_owner ON groups(owner_id)`,
		`CREATE TABLE IF NOT EXISTS group_members (group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE, user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (group_id, user_id))`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_group ON group_members(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id)`,
		`CREATE TABLE IF NOT EXISTS group_invite_requests (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE, from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(group_id, to_user_id))`,
		`CREATE INDEX IF NOT EXISTS idx_group_invites_to ON group_invite_requests(to_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_invites_from ON group_invite_requests(from_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_invites_group ON group_invite_requests(group_id)`,
	} {
		_, _ = pool.Exec(ctx, stmt)
	}

	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
