-- noinspection SqlResolveForFile @ table/"user_entity"

-- 004_user_created_at.sql
-- Add created_at column to user_entity to track when the user was created.

ALTER TABLE user_entity ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW();
