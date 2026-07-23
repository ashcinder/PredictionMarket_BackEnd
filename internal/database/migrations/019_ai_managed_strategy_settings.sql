ALTER TABLE ai_managed_entries
  ADD COLUMN strategy_buy_amount_bkc VARCHAR(78) NULL AFTER last_decision_text,
  ADD COLUMN strategy_confidence_min DECIMAL(7,6) NULL AFTER strategy_buy_amount_bkc,
  ADD COLUMN strategy_min_edge_percent DECIMAL(9,4) NULL AFTER strategy_confidence_min,
  ADD COLUMN strategy_kelly_fraction DECIMAL(7,6) NULL AFTER strategy_min_edge_percent,
  ADD COLUMN strategy_adaptive_cooldown BOOLEAN NULL AFTER strategy_kelly_fraction;
