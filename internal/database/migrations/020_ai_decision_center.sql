CREATE TABLE IF NOT EXISTS ai_settlement_audits (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  contract_address VARCHAR(42) NOT NULL,
  game_id BIGINT UNSIGNED NOT NULL,
  market_title VARCHAR(512) NOT NULL DEFAULT '',
  rule_summary TEXT NOT NULL,
  deterministic_candidate VARCHAR(16) NOT NULL DEFAULT '',
  evidence_json JSON NULL,
  opinions_json JSON NULL,
  final_decision VARCHAR(16) NOT NULL,
  final_confidence DECIMAL(7,6) NOT NULL DEFAULT 0,
  consensus_ratio DECIMAL(7,6) NOT NULL DEFAULT 0,
  final_summary TEXT NOT NULL,
  resolved_at TIMESTAMP(6) NOT NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  INDEX idx_ai_settlement_market (contract_address, game_id, resolved_at DESC),
  INDEX idx_ai_settlement_latest (resolved_at DESC),
  CONSTRAINT chk_ai_settlement_decision CHECK (
    final_decision IN ('YES', 'NO', 'INDETERMINATE')
  )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
