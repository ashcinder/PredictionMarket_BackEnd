CREATE TABLE IF NOT EXISTS gold_games (
	id BIGINT UNSIGNED NOT NULL,
	contract_address VARCHAR(42) NOT NULL,
	ipfs_cid VARCHAR(128) NOT NULL DEFAULT '',
	total_pool DECIMAL(65,0) NOT NULL DEFAULT 0,
	is_resolved TINYINT(1) NOT NULL DEFAULT 0,
	is_refunded TINYINT(1) NOT NULL DEFAULT 0,
	winning_option SMALLINT NOT NULL DEFAULT 0,
	deadline_sec BIGINT NOT NULL DEFAULT 0,
	reserve_yes DECIMAL(65,0) NOT NULL DEFAULT 0,
	reserve_no DECIMAL(65,0) NOT NULL DEFAULT 0,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	PRIMARY KEY (id),
	INDEX idx_gold_games_contract (contract_address),
	INDEX idx_gold_games_resolved (is_resolved),
	INDEX idx_gold_games_deadline (deadline_sec)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
CREATE TABLE IF NOT EXISTS gold_user_positions (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
	game_id BIGINT UNSIGNED NOT NULL,
	user_address VARCHAR(42) NOT NULL,
	shares_yes DECIMAL(65,0) NOT NULL DEFAULT 0,
	shares_no DECIMAL(65,0) NOT NULL DEFAULT 0,
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	PRIMARY KEY (id),
	UNIQUE KEY uk_game_user (game_id, user_address),
	INDEX idx_user_address (user_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
