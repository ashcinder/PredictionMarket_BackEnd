CREATE TABLE IF NOT EXISTS market_sync_state (
  contract_address VARCHAR(42) NOT NULL,
  game_id BIGINT UNSIGNED NOT NULL,
  last_success_at TIMESTAMP(6) NULL,
  last_observed_at BIGINT UNSIGNED NULL,
  fail_count INT UNSIGNED NOT NULL DEFAULT 0,
  next_poll_at TIMESTAMP(6) NULL,
  last_error VARCHAR(512) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'ok',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (contract_address, game_id),
  INDEX idx_market_sync_state_next_poll (status, next_poll_at),
  CONSTRAINT chk_market_sync_state_status CHECK (status IN ('ok','failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
ALTER TABLE ai_decisions DROP CHECK chk_ai_decisions_outcome;
-- migration:split
ALTER TABLE ai_decisions ADD CONSTRAINT chk_ai_decisions_outcome CHECK (outcome IN (
  'pending','history_insufficient','invalid_reserves','hold',
  'low_confidence','cooldown','traded','trade_failed',
  'sync_failed','sync_cooldown','metadata_unavailable','quote_unavailable'
));
