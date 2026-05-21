-- 008: Multi-tenancy — project-scoped users, waiting lists, and scheduler state.

-- Project table
CREATE TABLE IF NOT EXISTS project (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                    TEXT UNIQUE NOT NULL,
    name                    TEXT NOT NULL,
    entry_batch_size        INT,
    entry_window_interval   INTERVAL,
    waitlist_check_interval INTERVAL,
    scheduler_disabled      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert default project (idempotent)
INSERT INTO project (slug, name) VALUES ('default', 'Default')
ON CONFLICT (slug) DO NOTHING;

-- Add project_id to user_entity
ALTER TABLE user_entity ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE user_entity SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
ALTER TABLE user_entity ALTER COLUMN project_id SET NOT NULL;
DO $$ BEGIN
    ALTER TABLE user_entity ADD CONSTRAINT fk_user_entity_project FOREIGN KEY (project_id) REFERENCES project(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Rebuild unique constraint: email is unique per project
ALTER TABLE user_entity DROP CONSTRAINT IF EXISTS user_entity_email_key;
DROP INDEX IF EXISTS user_entity_email_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_entity_project_email ON user_entity (project_id, email);

-- Add project_id to waiting_list
ALTER TABLE waiting_list ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE waiting_list SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
ALTER TABLE waiting_list ALTER COLUMN project_id SET NOT NULL;
DO $$ BEGIN
    ALTER TABLE waiting_list ADD CONSTRAINT fk_waiting_list_project FOREIGN KEY (project_id) REFERENCES project(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Add project_id to scheduler_state
ALTER TABLE scheduler_state ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE scheduler_state SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
ALTER TABLE scheduler_state ALTER COLUMN project_id SET NOT NULL;
DO $$ BEGIN
    ALTER TABLE scheduler_state ADD CONSTRAINT fk_scheduler_state_project FOREIGN KEY (project_id) REFERENCES project(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Replace single-column PK with composite unique on (project_id, key)
ALTER TABLE scheduler_state DROP CONSTRAINT IF EXISTS scheduler_state_pkey;
CREATE UNIQUE INDEX IF NOT EXISTS uq_scheduler_state_project_key ON scheduler_state (project_id, key);

-- Performance indexes
CREATE INDEX IF NOT EXISTS idx_waiting_list_project_weighted ON waiting_list (project_id, weighted_created_at);
CREATE INDEX IF NOT EXISTS idx_user_entity_project_access ON user_entity (project_id, has_access);
