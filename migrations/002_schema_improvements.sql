-- noinspection SqlResolveForFile

-- 002_schema_improvements.sql
-- Mitigate table bloat on waiting_list and add performance indexes.

-- R3: Tune storage parameters for the high-churn waiting_list table.
-- Lower fillfactor reserves space for reuse after deletes; aggressive
-- autovacuum settings reclaim dead tuples promptly.
ALTER TABLE waiting_list SET (
    fillfactor = 70,
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 50
);

-- R4: Index weighted_created_at to support ORDER BY queries in GetWithOffsetLimit.
CREATE INDEX IF NOT EXISTS idx_waiting_list_weighted_created_at
    ON waiting_list (weighted_created_at ASC);
