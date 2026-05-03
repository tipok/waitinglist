-- noinspection SqlResolveForFile @ table/"user_entity"
-- 006_has_access_one_way.sql
-- Enforce that user_entity.has_access can only transition false → true.
-- Once a user is granted access, the flag may never be revoked.

CREATE OR REPLACE FUNCTION user_entity_has_access_one_way()
RETURNS trigger AS $$
BEGIN
    IF OLD.has_access = TRUE AND NEW.has_access = FALSE THEN
        RAISE EXCEPTION
            'has_access is one-way: cannot set false on user % whose has_access is already true', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_user_entity_has_access_one_way ON user_entity;
CREATE TRIGGER trg_user_entity_has_access_one_way
    BEFORE UPDATE ON user_entity
    FOR EACH ROW
    EXECUTE FUNCTION user_entity_has_access_one_way();
