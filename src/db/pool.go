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
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikirs ADD COLUMN IF NOT EXISTS mode TEXT DEFAULT 'pooled'`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikirs ADD COLUMN IF NOT EXISTS target_count INT DEFAULT 100`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikir_requests ADD COLUMN IF NOT EXISTS mode TEXT DEFAULT 'pooled'`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikir_requests ADD COLUMN IF NOT EXISTS target_count INT DEFAULT 100`)
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
		// Custom zikirs (user-created)
		`CREATE TABLE IF NOT EXISTS custom_zikirs (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, name_tr TEXT NOT NULL, name_en TEXT DEFAULT '', read_tr TEXT DEFAULT '', arabic TEXT NOT NULL, translation_tr TEXT DEFAULT '', translation_en TEXT DEFAULT '', description_tr TEXT DEFAULT '', description_en TEXT DEFAULT '', target_count INT NOT NULL DEFAULT 33, category TEXT DEFAULT '', tags JSONB DEFAULT '[]', created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE INDEX IF NOT EXISTS idx_custom_zikirs_user ON custom_zikirs(user_id)`,
		// Group zikirs (built-in or custom ref; mode: pooled=one total, individual=per-person)
		`CREATE TABLE IF NOT EXISTS group_zikirs (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE, zikir_type TEXT NOT NULL CHECK (zikir_type IN ('builtin','custom')), zikir_ref TEXT NOT NULL, mode TEXT NOT NULL DEFAULT 'pooled' CHECK (mode IN ('pooled','individual')), target_count INT NOT NULL DEFAULT 100, added_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikirs_group ON group_zikirs(group_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_group_zikirs_unique ON group_zikirs(group_id, zikir_type, zikir_ref)`,
		// Group zikir progress: pooled uses user_id='00000000-0000-0000-0000-000000000000', individual uses real user_id
		`CREATE TABLE IF NOT EXISTS group_zikir_progress (group_zikir_id UUID NOT NULL REFERENCES group_zikirs(id) ON DELETE CASCADE, user_id UUID NOT NULL, reads INT NOT NULL DEFAULT 0, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (group_zikir_id, user_id))`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikir_progress_gz ON group_zikir_progress(group_zikir_id)`,
		// Friend zikir requests (send to friend, they accept/refuse)
		`CREATE TABLE IF NOT EXISTS friend_zikir_requests (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, zikir_type TEXT NOT NULL CHECK (zikir_type IN ('builtin','custom')), zikir_ref TEXT NOT NULL, target_count INT NOT NULL DEFAULT 33, status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')), created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikir_requests_to ON friend_zikir_requests(to_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikir_requests_from ON friend_zikir_requests(from_user_id)`,
		// Friend zikirs (accepted; to_user can read, from_user can see progress)
		`CREATE TABLE IF NOT EXISTS friend_zikirs (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), request_id UUID NOT NULL UNIQUE REFERENCES friend_zikir_requests(id) ON DELETE CASCADE, to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, zikir_type TEXT NOT NULL, zikir_ref TEXT NOT NULL, target_count INT NOT NULL DEFAULT 33, reads INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikirs_to ON friend_zikirs(to_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikirs_from ON friend_zikirs(from_user_id)`,
		// Group zikir requests (member suggests, owner approves)
		`CREATE TABLE IF NOT EXISTS group_zikir_requests (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE, from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE, zikir_type TEXT NOT NULL CHECK (zikir_type IN ('builtin','custom')), zikir_ref TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')), created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikir_requests_group ON group_zikir_requests(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikir_requests_from ON group_zikir_requests(from_user_id)`,
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
