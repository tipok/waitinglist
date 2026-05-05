-- noinspection SqlResolveForFile @ table/"user_entity"
-- 007_access_audit_and_drop_one_way.sql
-- Add audit columns for access grants/revocations and remove the one-way
-- has_access trigger introduced in migration 006. Application code is now
-- the source of truth for the false→true→false transitions: only
-- UserRepository.RevokeAccessTx may flip has_access back to false, and only
-- with a non-empty reason.

ALTER TABLE user_entity
    ADD COLUMN IF NOT EXISTS access_granted_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS access_granted_by    TEXT,
    ADD COLUMN IF NOT EXISTS access_revoked_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS access_revoked_by    TEXT,
    ADD COLUMN IF NOT EXISTS access_revoke_reason TEXT;

-- Backfill: any user that already has access was granted by the scheduler at
-- some point. The exact timestamp is unrecoverable; created_at is the best
-- conservative proxy.
UPDATE user_entity
SET    access_granted_at = COALESCE(access_granted_at, created_at),
       access_granted_by = COALESCE(access_granted_by, 'scheduler')
WHERE  has_access = TRUE
   AND (access_granted_at IS NULL OR access_granted_by IS NULL);

-- Constraints (added after backfill so existing data validates).
ALTER TABLE user_entity
    DROP CONSTRAINT IF EXISTS user_entity_access_granted_by_check;
ALTER TABLE user_entity
    ADD  CONSTRAINT user_entity_access_granted_by_check
         CHECK (access_granted_by IS NULL OR access_granted_by IN ('scheduler','admin'));

ALTER TABLE user_entity
    DROP CONSTRAINT IF EXISTS user_entity_revoke_pair_check;
ALTER TABLE user_entity
    ADD  CONSTRAINT user_entity_revoke_pair_check
         CHECK ((access_revoked_at IS NULL) = (access_revoke_reason IS NULL));

-- Drop the one-way trigger from migration 006. Revocation is now allowed
-- at the SQL level; the application enforces "only via RevokeAccessTx".
DROP TRIGGER  IF EXISTS trg_user_entity_has_access_one_way ON user_entity;
DROP FUNCTION IF EXISTS user_entity_has_access_one_way();

-- Index supporting the per-day enlistment chart used by the admin dashboard
-- (plan 18).
CREATE INDEX IF NOT EXISTS idx_user_entity_created_at
    ON user_entity (created_at);
