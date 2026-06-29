ALTER TABLE gold_games ADD COLUMN deadline_sec BIGINT NOT NULL DEFAULT 0 AFTER creator_address;
