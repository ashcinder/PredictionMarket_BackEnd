CREATE TABLE IF NOT EXISTS ai_managed_entries (
  contract_address VARCHAR(42) NOT NULL,
  game_id BIGINT UNSIGNED NOT NULL,
  user_address VARCHAR(42) NOT NULL,
  key_nonce VARBINARY(32) NOT NULL,
  key_ciphertext VARBINARY(512) NOT NULL,
  enabled_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  last_trade_at TIMESTAMP(6) NULL,
  last_trade_option TINYINT NOT NULL DEFAULT -1,
  last_trade_tx VARCHAR(80) NOT NULL DEFAULT '',
  last_error VARCHAR(512) NOT NULL DEFAULT '',
  last_decision_at TIMESTAMP(6) NULL,
  last_decision_text TEXT,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (contract_address, game_id, user_address),
  INDEX idx_ai_managed_entries_user (user_address, enabled_at),
  INDEX idx_ai_managed_entries_market (contract_address, game_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
