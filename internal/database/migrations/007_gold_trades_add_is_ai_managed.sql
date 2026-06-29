ALTER TABLE gold_trades ADD COLUMN is_ai_managed TINYINT(1) NOT NULL DEFAULT 0;
-- migration:split
CREATE INDEX idx_gold_trades_game_user ON gold_trades(game_id, user_address);
