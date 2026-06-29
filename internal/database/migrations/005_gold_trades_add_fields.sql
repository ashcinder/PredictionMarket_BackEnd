ALTER TABLE gold_trades ADD COLUMN shares_wei VARBINARY(80) NULL AFTER amount_wei;
-- migration:split
ALTER TABLE gold_trades ADD COLUMN price_at_trade DOUBLE NULL AFTER shares_wei;
-- migration:split
ALTER TABLE gold_trades ADD COLUMN timestamp_sec BIGINT NOT NULL DEFAULT 0 AFTER price_at_trade;
-- migration:split
ALTER TABLE gold_trades ADD INDEX idx_trades_game_user_time (game_id, user_address, timestamp_sec DESC);
