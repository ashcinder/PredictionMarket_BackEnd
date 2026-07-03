CREATE TABLE IF NOT EXISTS gold_games (
    game_id INTEGER NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    ipfs_cid VARCHAR(128) NOT NULL,
    `desc` TEXT,
    `condition` TEXT,
    avatar_url VARCHAR(256) DEFAULT '',
    detailed_info TEXT,
    option_yes VARCHAR(64) DEFAULT 'YES',
    option_no VARCHAR(64) DEFAULT 'NO',
    creator_address VARCHAR(42) DEFAULT '',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (game_id),
    INDEX idx_gold_games_cid (ipfs_cid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
CREATE TABLE IF NOT EXISTS gold_chain_states (
    game_id INTEGER NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    total_pool VARBINARY(80) NULL,
    is_resolved TINYINT(1) NOT NULL DEFAULT 0,
    is_refunded TINYINT(1) NOT NULL DEFAULT 0,
    winning_option TINYINT NOT NULL DEFAULT 0,
    deadline_sec BIGINT NOT NULL DEFAULT 0,
    reserve_yes VARBINARY(80) NULL,
    reserve_no VARBINARY(80) NULL,
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (contract_address, game_id),
    INDEX idx_gold_chain_states_game (game_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
CREATE TABLE IF NOT EXISTS gold_user_positions (
    user_address VARCHAR(42) NOT NULL,
    game_id INTEGER NOT NULL,
    my_shares_yes VARBINARY(80) NULL,
    my_shares_no VARBINARY(80) NULL,
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_address, game_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
CREATE TABLE IF NOT EXISTS gold_price_history (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    game_id INTEGER NOT NULL,
    timestamp_sec BIGINT NOT NULL,
    yes_price DECIMAL(9,6) NOT NULL,
    no_price DECIMAL(9,6) NOT NULL,
    total_pool VARBINARY(80) NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    INDEX idx_history_game_time (game_id, timestamp_sec DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- migration:split
CREATE TABLE IF NOT EXISTS gold_trades (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    game_id INTEGER NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    user_address VARCHAR(42) NOT NULL,
    trade_type VARCHAR(10) NOT NULL,
    option_id TINYINT NOT NULL DEFAULT 0,
    amount_wei VARBINARY(80) NULL,
    tx_hash VARCHAR(80) NOT NULL DEFAULT '',
    is_success TINYINT(1) NOT NULL DEFAULT 0,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    INDEX idx_trades_game (game_id, created_at DESC),
    INDEX idx_trades_user (user_address, created_at DESC),
    CONSTRAINT chk_trades_type CHECK (trade_type IN ('BUY','SELL','CLAIM','RESOLVE'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
