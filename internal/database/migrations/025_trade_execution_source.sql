ALTER TABLE gold_trades
  ADD COLUMN execution_source VARCHAR(16) NOT NULL DEFAULT 'manual' AFTER is_ai_managed;

-- migration:split
UPDATE gold_trades AS trade
LEFT JOIN ai_managed_entries AS managed
  ON managed.contract_address = trade.contract_address
 AND managed.game_id = trade.game_id
 AND LOWER(managed.user_address) = LOWER(trade.user_address)
SET trade.execution_source = CASE
  WHEN trade.is_ai_managed = 0 THEN 'manual'
  WHEN managed.strategy_type = 'grid' THEN 'grid'
  WHEN managed.strategy_type = 'martingale' THEN 'martingale'
  ELSE 'ai'
END;
