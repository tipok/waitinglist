ALTER TABLE waiting_list
    ADD COLUMN IF NOT EXISTS ip_address INET;
