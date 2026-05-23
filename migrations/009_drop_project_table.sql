-- noinspection SqlResolveForFile

-- 009: Eliminate project table — switch to config-only multi-tenancy with project_slug TEXT.

-- Step 1: Add project_slug TEXT columns
ALTER TABLE user_entity ADD COLUMN IF NOT EXISTS project_slug TEXT;
ALTER TABLE waiting_list ADD COLUMN IF NOT EXISTS project_slug TEXT;
ALTER TABLE scheduler_state ADD COLUMN IF NOT EXISTS project_slug TEXT;

-- Step 2: Backfill from project table (only if project table still exists)
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'project') THEN
        UPDATE user_entity SET project_slug = p.slug FROM project p WHERE user_entity.project_id = p.id AND user_entity.project_slug IS NULL;
        UPDATE waiting_list SET project_slug = p.slug FROM project p WHERE waiting_list.project_id = p.id AND waiting_list.project_slug IS NULL;
        UPDATE scheduler_state SET project_slug = p.slug FROM project p WHERE scheduler_state.project_id = p.id AND scheduler_state.project_slug IS NULL;
    END IF;
END $$;

-- Step 3: Set NOT NULL (idempotent — only if currently nullable)
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'user_entity' AND column_name = 'project_slug' AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE user_entity ALTER COLUMN project_slug SET NOT NULL;
    END IF;
END $$;
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'waiting_list' AND column_name = 'project_slug' AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE waiting_list ALTER COLUMN project_slug SET NOT NULL;
    END IF;
END $$;
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'scheduler_state' AND column_name = 'project_slug' AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE scheduler_state ALTER COLUMN project_slug SET NOT NULL;
    END IF;
END $$;

-- Step 4: Drop old project_id FK constraints and columns
ALTER TABLE user_entity DROP CONSTRAINT IF EXISTS fk_user_entity_project;
ALTER TABLE waiting_list DROP CONSTRAINT IF EXISTS fk_waiting_list_project;
ALTER TABLE scheduler_state DROP CONSTRAINT IF EXISTS fk_scheduler_state_project;

ALTER TABLE user_entity DROP COLUMN IF EXISTS project_id;
ALTER TABLE waiting_list DROP COLUMN IF EXISTS project_id;
ALTER TABLE scheduler_state DROP COLUMN IF EXISTS project_id;

-- Step 5: Recreate indexes and constraints using project_slug
DROP INDEX IF EXISTS uq_user_entity_project_email;
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_entity_project_slug_email ON user_entity (project_slug, email);

DROP INDEX IF EXISTS idx_user_entity_project_access;
CREATE INDEX IF NOT EXISTS idx_user_entity_project_slug_access ON user_entity (project_slug, has_access);

DROP INDEX IF EXISTS idx_waiting_list_project_weighted;
CREATE INDEX IF NOT EXISTS idx_waiting_list_project_slug_weighted ON waiting_list (project_slug, weighted_created_at);

-- Rebuild scheduler_state PK: (project_slug, key)
ALTER TABLE scheduler_state DROP CONSTRAINT IF EXISTS scheduler_state_pkey;
DO $$ BEGIN
    ALTER TABLE scheduler_state ADD PRIMARY KEY (project_slug, key);
EXCEPTION WHEN duplicate_object THEN NULL;
         WHEN invalid_table_definition THEN NULL;
END $$;

-- Step 6: Drop the project table
DROP TABLE IF EXISTS project;
