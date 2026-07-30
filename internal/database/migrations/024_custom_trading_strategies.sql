ALTER TABLE ai_managed_entries
  ADD COLUMN strategy_type VARCHAR(24) NOT NULL DEFAULT 'ai' AFTER strategy_adaptive_cooldown,
  ADD COLUMN strategy_direction VARCHAR(8) NOT NULL DEFAULT 'yes' AFTER strategy_type,
  ADD COLUMN strategy_grid_lower_percent DECIMAL(9,4) NULL AFTER strategy_direction,
  ADD COLUMN strategy_grid_upper_percent DECIMAL(9,4) NULL AFTER strategy_grid_lower_percent,
  ADD COLUMN strategy_grid_levels INT NULL AFTER strategy_grid_upper_percent,
  ADD COLUMN strategy_martingale_trigger_percent DECIMAL(9,4) NULL AFTER strategy_grid_levels,
  ADD COLUMN strategy_martingale_multiplier DECIMAL(9,4) NULL AFTER strategy_martingale_trigger_percent,
  ADD COLUMN strategy_martingale_max_rounds INT NULL AFTER strategy_martingale_multiplier;
