CREATE TABLE IF NOT EXISTS gold_portfolio_history (
    user_address VARCHAR(42) NOT NULL,
    timestamp_sec BIGINT NOT NULL,
    total_value_wei VARBINARY(80) NOT NULL,
    active_market_count INT UNSIGNED NOT NULL DEFAULT 0,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_address, timestamp_sec),
    INDEX idx_portfolio_history_user_time (user_address, timestamp_sec DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
