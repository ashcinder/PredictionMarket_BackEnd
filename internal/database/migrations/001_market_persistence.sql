CREATE TABLE IF NOT EXISTS market_history (
  contract_address VARCHAR(42) NOT NULL,
  game_id BIGINT UNSIGNED NOT NULL,
  observed_at BIGINT UNSIGNED NOT NULL,
  yes_percent DECIMAL(9,6) NOT NULL,
  no_percent DECIMAL(9,6) NOT NULL,
  reserve_no VARBINARY(32) NULL,
  reserve_yes VARBINARY(32) NULL,
  source VARCHAR(16) NOT NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (contract_address, game_id, observed_at),
  INDEX idx_market_history_latest (contract_address, game_id, observed_at DESC),
  CONSTRAINT chk_market_history_source CHECK (source IN ('chain','ipfs')),
  CONSTRAINT chk_market_history_percent CHECK (
    yes_percent BETWEEN 0 AND 100 AND no_percent BETWEEN 0 AND 100
  )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
CREATE TABLE IF NOT EXISTS ai_decisions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  contract_address VARCHAR(42) NOT NULL,
  game_id BIGINT UNSIGNED NOT NULL,
  user_address VARCHAR(42) NOT NULL,
  observed_at BIGINT UNSIGNED NOT NULL,
  decision_source VARCHAR(16) NOT NULL,
  action VARCHAR(16) NOT NULL,
  confidence DECIMAL(7,6) NOT NULL,
  reason TEXT NOT NULL,
  history_points INT UNSIGNED NOT NULL,
  outcome VARCHAR(32) NOT NULL,
  tx_hash VARCHAR(80) NOT NULL DEFAULT '',
  error_summary VARCHAR(512) NOT NULL DEFAULT '',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  INDEX idx_ai_decisions_market (contract_address, game_id, observed_at DESC),
  INDEX idx_ai_decisions_user (user_address, observed_at DESC),
  CONSTRAINT chk_ai_decisions_source CHECK (decision_source IN ('rule','model')),
  CONSTRAINT chk_ai_decisions_action CHECK (action IN ('buy_yes','buy_no','hold')),
  CONSTRAINT chk_ai_decisions_outcome CHECK (outcome IN (
    'pending','history_insufficient','invalid_reserves','hold',
    'low_confidence','cooldown','traded','trade_failed'
  ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
