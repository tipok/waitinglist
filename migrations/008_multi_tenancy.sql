-- noinspection SqlResolveForFile

-- noinspection SqlResolveForFile @ table/"user_entity"

-- 008: Multi-tenancy — project-scoped users, waiting lists, and scheduler state.

-- Project table
CREATE TABLE IF NOT EXISTS project (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                    TEXT UNIQUE NOT NULL,
    name                    TEXT NOT NULL,
    entry_batch_size        INT,
    entry_window_interval   TEXT,
    waitlist_check_interval TEXT,
    scheduler_disabled      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert default project (idempotent)
INSERT INTO project (slug, name) VALUES ('default', 'Default')
ON CONFLICT (slug) DO NOTHING;

-- Add project_id to user_entity
ALTER TABLE user_entity ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE user_entity SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_entity' AND column_name = 'project_id' AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE user_entity ALTER COLUMN project_id SET NOT NULL;
    END IF;
END $$;
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
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'waiting_list' AND column_name = 'project_id' AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE waiting_list ALTER COLUMN project_id SET NOT NULL;
    END IF;
END $$;
DO $$ BEGIN
    ALTER TABLE waiting_list ADD CONSTRAINT fk_waiting_list_project FOREIGN KEY (project_id) REFERENCES project(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Add project_id to scheduler_state
ALTER TABLE scheduler_state ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE scheduler_state SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'scheduler_state' AND column_name = 'project_id' AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE scheduler_state ALTER COLUMN project_id SET NOT NULL;
    END IF;
END $$;
DO $$ BEGIN
    ALTER TABLE scheduler_state ADD CONSTRAINT fk_scheduler_state_project FOREIGN KEY (project_id) REFERENCES project(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Replace single-column PK with composite primary key on (project_id, key)
ALTER TABLE scheduler_state DROP CONSTRAINT IF EXISTS scheduler_state_pkey;
DO $$ BEGIN
    ALTER TABLE scheduler_state ADD PRIMARY KEY (project_id, key);
EXCEPTION WHEN duplicate_table THEN NULL;
         WHEN invalid_table_definition THEN NULL;
END $$;

-- Performance indexes
CREATE INDEX IF NOT EXISTS idx_waiting_list_project_weighted ON waiting_list (project_id, weighted_created_at);
CREATE INDEX IF NOT EXISTS idx_user_entity_project_access ON user_entity (project_id, has_access);
CREATE INDEX IF NOT EXISTS idx_waiting_list_user_id ON waiting_list (user_id);
