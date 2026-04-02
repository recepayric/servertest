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

	// Schema init / auto-migrate.
	// Important: create base tables FIRST. The previous implementation used ALTERs before CREATE
	// which could silently fail and leave columns missing on a fresh DB.
	//
	// Note: we keep this idempotent (IF NOT EXISTS) so redeploys are safe.
	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		// gen_random_uuid() may still exist if the extension is already enabled; otherwise later
		// statements will fail and we will return the error.
		log.Printf("⚠️  pgcrypto extension create failed (may already exist): %v", err)
	}

	stmts := []string{
		// Users
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			guest_token TEXT NOT NULL UNIQUE,
			friend_code TEXT NOT NULL UNIQUE,
			display_name TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,

		// Identity mapping (Unity Authentication, etc.)
		`CREATE TABLE IF NOT EXISTS user_identities (
			provider TEXT NOT NULL,
			external_id TEXT NOT NULL,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY(provider, external_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_identities_user ON user_identities(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_identities_user_provider ON user_identities(user_id, provider)`,

		// Groups
		`CREATE TABLE IF NOT EXISTS groups (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			icon_index INTEGER NOT NULL DEFAULT -1,
			icon_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_groups_owner ON groups(owner_id)`,

		`CREATE TABLE IF NOT EXISTS group_members (
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (group_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_group ON group_members(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id)`,

		`CREATE TABLE IF NOT EXISTS group_invite_requests (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(group_id, to_user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_group_invites_to ON group_invite_requests(to_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_invites_from ON group_invite_requests(from_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_invites_group ON group_invite_requests(group_id)`,

		// Custom zikirs (user-created)
		`CREATE TABLE IF NOT EXISTS custom_zikirs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name_tr TEXT NOT NULL,
			name_en TEXT DEFAULT '',
			read_tr TEXT DEFAULT '',
			arabic TEXT NOT NULL,
			translation_tr TEXT DEFAULT '',
			translation_en TEXT DEFAULT '',
			description_tr TEXT DEFAULT '',
			description_en TEXT DEFAULT '',
			target_count INT NOT NULL DEFAULT 33,
			category TEXT DEFAULT '',
			tags JSONB DEFAULT '[]',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_zikirs_user ON custom_zikirs(user_id)`,

		// Group zikir requests (member suggests, owner approves)
		`CREATE TABLE IF NOT EXISTS group_zikir_requests (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			zikir_type TEXT NOT NULL CHECK (zikir_type IN ('builtin','custom')),
			zikir_ref TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'pooled' CHECK (mode IN ('pooled','individual')),
			target_count INT NOT NULL DEFAULT 100,
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')),
			accepted_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			refused_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
			group_zikir_id UUID,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikir_requests_group ON group_zikir_requests(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikir_requests_from ON group_zikir_requests(from_user_id)`,

		// Per-member accept/refuse: each member responds individually to a request
		`CREATE TABLE IF NOT EXISTS group_zikir_request_responses (
			request_id UUID NOT NULL REFERENCES group_zikir_requests(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			response TEXT NOT NULL CHECK (response IN ('accepted','refused')),
			reads INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (request_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gzrr_request ON group_zikir_request_responses(request_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gzrr_user ON group_zikir_request_responses(user_id)`,

		// Group zikirs (built-in or custom ref; mode: pooled=one total, individual=per-person)
		// request_id links to the group_zikir_requests (optional / nullable).
		`CREATE TABLE IF NOT EXISTS group_zikirs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			zikir_type TEXT NOT NULL CHECK (zikir_type IN ('builtin','custom')),
			zikir_ref TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'pooled' CHECK (mode IN ('pooled','individual')),
			target_count INT NOT NULL DEFAULT 100,
			added_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			request_id UUID REFERENCES group_zikir_requests(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikirs_group ON group_zikirs(group_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_group_zikirs_unique ON group_zikirs(group_id, zikir_type, zikir_ref)`,

		// Group zikir progress: pooled uses user_id='00000000-0000-0000-0000-000000000000', individual uses real user_id
		`CREATE TABLE IF NOT EXISTS group_zikir_progress (
			group_zikir_id UUID NOT NULL REFERENCES group_zikirs(id) ON DELETE CASCADE,
			user_id UUID NOT NULL,
			reads INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (group_zikir_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_group_zikir_progress_gz ON group_zikir_progress(group_zikir_id)`,

		// Friend zikir requests (send to friend, they accept/refuse)
		`CREATE TABLE IF NOT EXISTS friend_zikir_requests (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			zikir_type TEXT NOT NULL CHECK (zikir_type IN ('builtin','custom')),
			zikir_ref TEXT NOT NULL,
			target_count INT NOT NULL DEFAULT 33,
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikir_requests_to ON friend_zikir_requests(to_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikir_requests_from ON friend_zikir_requests(from_user_id)`,

		// Friend zikirs (accepted; to_user can read, from_user can see progress)
		`CREATE TABLE IF NOT EXISTS friend_zikirs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			request_id UUID NOT NULL UNIQUE REFERENCES friend_zikir_requests(id) ON DELETE CASCADE,
			to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			zikir_type TEXT NOT NULL,
			zikir_ref TEXT NOT NULL,
			target_count INT NOT NULL DEFAULT 33,
			reads INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			completed_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikirs_to ON friend_zikirs(to_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikirs_from ON friend_zikirs(from_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikirs_updated_at ON friend_zikirs(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_friend_zikirs_completed_at ON friend_zikirs(completed_at)`,
	}

	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("schema migration failed: %w", err)
		}
	}

	// Backward-compatible column tweaks for DBs created by older versions of this code.
	// (These are safe even after the CREATE TABLEs above.)
	_, _ = pool.Exec(ctx, `ALTER TABLE groups ADD COLUMN IF NOT EXISTS icon_index INTEGER NOT NULL DEFAULT -1`)
	_, _ = pool.Exec(ctx, `ALTER TABLE groups ADD COLUMN IF NOT EXISTS icon_key TEXT NOT NULL DEFAULT ''`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikirs ADD COLUMN IF NOT EXISTS request_id UUID`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikirs ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'pooled'`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikirs ADD COLUMN IF NOT EXISTS target_count INT NOT NULL DEFAULT 100`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikir_requests ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'pooled'`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikir_requests ADD COLUMN IF NOT EXISTS target_count INT NOT NULL DEFAULT 100`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikir_requests ADD COLUMN IF NOT EXISTS accepted_by_user_id UUID`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikir_requests ADD COLUMN IF NOT EXISTS refused_by_user_id UUID`)
	_, _ = pool.Exec(ctx, `ALTER TABLE group_zikir_requests ADD COLUMN IF NOT EXISTS group_zikir_id UUID`)
	_, _ = pool.Exec(ctx, `ALTER TABLE friend_zikirs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`)
	_, _ = pool.Exec(ctx, `ALTER TABLE friend_zikirs ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ`)
	_, _ = pool.Exec(ctx, `ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT`)

	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
