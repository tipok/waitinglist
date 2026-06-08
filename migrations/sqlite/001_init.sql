CREATE TABLE IF NOT EXISTS user_entity
(
    id                   TEXT    NOT NULL PRIMARY KEY,
    firstname            TEXT    NOT NULL,
    lastname             TEXT    NOT NULL,
    email                TEXT    NOT NULL,
    has_access           INTEGER NOT NULL DEFAULT 0,
    created_at           TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    ip_address           TEXT,
    access_granted_at    TEXT,
    access_granted_by    TEXT
        CHECK (access_granted_by IS NULL OR access_granted_by IN ('scheduler', 'admin')),
    access_revoked_at    TEXT,
    access_revoked_by    TEXT,
    access_revoke_reason TEXT,
    project_slug         TEXT    NOT NULL,
    CONSTRAINT user_entity_revoke_pair_check
        CHECK ((access_revoked_at IS NULL) = (access_revoke_reason IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_user_entity_created_at
    ON user_entity (created_at);

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_entity_project_slug_email
    ON user_entity (project_slug, email);

CREATE INDEX IF NOT EXISTS idx_user_entity_project_slug_access
    ON user_entity (project_slug, has_access);

CREATE TABLE IF NOT EXISTS waiting_list
(
    id                  TEXT    NOT NULL PRIMARY KEY,
    user_id             TEXT    NOT NULL UNIQUE
        REFERENCES user_entity (id) ON DELETE CASCADE,
    created_at          TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    weight              INTEGER NOT NULL DEFAULT 0,
    weighted_created_at TEXT GENERATED ALWAYS AS (datetime(created_at, '-' || (weight * 3600) || ' seconds')) STORED,
    project_slug        TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_waiting_list_weighted_created_at
    ON waiting_list (weighted_created_at);

CREATE INDEX IF NOT EXISTS idx_waiting_list_user_id
    ON waiting_list (user_id);

CREATE INDEX IF NOT EXISTS idx_waiting_list_project_slug_weighted
    ON waiting_list (project_slug, weighted_created_at);

CREATE TABLE IF NOT EXISTS scheduler_state
(
    key          TEXT NOT NULL,
    value        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    project_slug TEXT NOT NULL,
    PRIMARY KEY (project_slug, key)
);
