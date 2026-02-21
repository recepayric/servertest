-- Add display_name to users if missing. Run: psql $DATABASE_URL -f migrations/003_users_display_name.sql

ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT;
