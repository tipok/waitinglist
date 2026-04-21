-- noinspection SqlResolveForFile @ table/"user_entity"
ALTER TABLE user_entity
    ADD COLUMN IF NOT EXISTS ip_address INET;
