CREATE TABLE IF NOT EXISTS user_entity (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    firstname  VARCHAR(255) NOT NULL,
    lastname   VARCHAR(255) NOT NULL,
    email      VARCHAR(255) NOT NULL UNIQUE,
    has_access BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS waiting_list (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES user_entity(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    weight     INTEGER NOT NULL DEFAULT 0,
    weighted_created_at TIMESTAMP GENERATED ALWAYS AS (created_at - INTERVAL '1 hour' * weight) STORED,
    UNIQUE(user_id)
);
