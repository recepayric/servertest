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
		`CREATE INDEX IF NOT EXISTS idx_custom_zikirs_user_updated ON custom_zikirs(user_id, created_at DESC)`,

		// Premium entitlement + cloud-save profile.
		// Server should enforce premium checks in handlers before allowing cloud writes.
		`CREATE TABLE IF NOT EXISTS user_entitlements (
			user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			is_premium BOOLEAN NOT NULL DEFAULT false,
			plan TEXT NOT NULL DEFAULT 'normal',
			expires_at TIMESTAMPTZ,
			has_no_ads BOOLEAN NOT NULL DEFAULT false,
			last_verified_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_entitlements_premium ON user_entitlements(is_premium)`,
		`CREATE INDEX IF NOT EXISTS idx_user_entitlements_expires ON user_entitlements(expires_at)`,

		// Snapshot stats for fast profile fetch.
		`CREATE TABLE IF NOT EXISTS user_profile_stats (
			user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			total_reads BIGINT NOT NULL DEFAULT 0,
			level INT NOT NULL DEFAULT 1,
			xp BIGINT NOT NULL DEFAULT 0,
			streak_days INT NOT NULL DEFAULT 0,
			best_streak_days INT NOT NULL DEFAULT 0,
			daily_target INT NOT NULL DEFAULT 33,
			daily_reads INT NOT NULL DEFAULT 0,
			last_read_at TIMESTAMPTZ,
			last_daily_reset_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,

		// Daily rollups to support streak and charts.
		`CREATE TABLE IF NOT EXISTS user_daily_stats (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			day DATE NOT NULL,
			reads INT NOT NULL DEFAULT 0,
			completed BOOLEAN NOT NULL DEFAULT false,
			PRIMARY KEY (user_id, day)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_daily_stats_user_day ON user_daily_stats(user_id, day DESC)`,

		`CREATE TABLE IF NOT EXISTS user_daily_meta (
			user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			last_completed_date DATE,
			current_streak INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,

		// Custom routines that users create/edit/delete on client.
		`CREATE TABLE IF NOT EXISTS user_routines (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			background_color TEXT DEFAULT '',
			icon_color TEXT DEFAULT '',
			style_key TEXT DEFAULT '',
			theme_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			icon_key TEXT DEFAULT '',
			icon_file TEXT DEFAULT '',
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_routines_user ON user_routines(user_id, sort_order, created_at DESC)`,

		`CREATE TABLE IF NOT EXISTS user_routine_items (
			routine_id UUID NOT NULL REFERENCES user_routines(id) ON DELETE CASCADE,
			zikir_id TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'added' CHECK (source IN ('base', 'added')),
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (routine_id, zikir_id, source)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_routine_items_routine ON user_routine_items(routine_id, source, sort_order)`,

		// Per-zikir user progress map (matches user_progress.json model).
		`CREATE TABLE IF NOT EXISTS user_zikir_progress (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			zikir_key TEXT NOT NULL,
			clicks INT NOT NULL DEFAULT 0,
			repeats INT NOT NULL DEFAULT 0,
			target_count INT NOT NULL DEFAULT 0,
			is_favourite BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, zikir_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_zikir_progress_user_updated ON user_zikir_progress(user_id, updated_at DESC)`,

		// Achievements map (matches achievements.json model).
		`CREATE TABLE IF NOT EXISTS user_achievements (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			achievement_id TEXT NOT NULL,
			current_value INT NOT NULL DEFAULT 0,
			unlocked BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, achievement_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_achievements_user_updated ON user_achievements(user_id, updated_at DESC)`,

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
	_, _ = pool.Exec(ctx, `ALTER TABLE user_daily_stats ADD COLUMN IF NOT EXISTS completed BOOLEAN NOT NULL DEFAULT false`)
	_, _ = pool.Exec(ctx, `ALTER TABLE user_routines ADD COLUMN IF NOT EXISTS theme_json JSONB NOT NULL DEFAULT '{}'::jsonb`)

	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
