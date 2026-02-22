package main

import (
	"context"
	"log"
	"strings"
	"time"

	"servertest/db"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.Init(ctx); err != nil {
		log.Fatalf("DB init failed: %v", err)
	}
	defer db.Close()

	sql := `
CREATE TABLE IF NOT EXISTS friendships (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    friend_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, friend_id),
    CHECK (user_id != friend_id)
);
CREATE INDEX IF NOT EXISTS idx_friendships_user_id ON friendships(user_id);
CREATE INDEX IF NOT EXISTS idx_friendships_friend_id ON friendships(friend_id);

CREATE TABLE IF NOT EXISTS friendship_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_groups_owner ON groups(owner_id);

CREATE TABLE IF NOT EXISTS group_members (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_group_members_group ON group_members(group_id);
CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);

CREATE TABLE IF NOT EXISTS group_invite_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(group_id, to_user_id)
);
CREATE INDEX IF NOT EXISTS idx_group_invites_to ON group_invite_requests(to_user_id);
CREATE INDEX IF NOT EXISTS idx_group_invites_from ON group_invite_requests(from_user_id);
CREATE INDEX IF NOT EXISTS idx_group_invites_group ON group_invite_requests(group_id);

CREATE INDEX IF NOT EXISTS idx_friendship_requests_to_user ON friendship_requests(to_user_id);
CREATE INDEX IF NOT EXISTS idx_friendship_requests_from_user ON friendship_requests(from_user_id);

ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT;
`
	for _, stmt := range strings.Split(sql, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Pool.Exec(ctx, stmt); err != nil {
			log.Printf("Warning: %v (may already exist)", err)
		} else {
			log.Printf("OK: %s", strings.Split(stmt, "(")[0])
		}
	}
	log.Println("Migration done.")
}
