-- Friend request flow: A sends request to B, B accepts/refuses.

CREATE TABLE IF NOT EXISTS friendship_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','refused')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_friendship_requests_to_user ON friendship_requests(to_user_id);
CREATE INDEX IF NOT EXISTS idx_friendship_requests_from_user ON friendship_requests(from_user_id);
