CREATE TABLE IF NOT EXISTS oracle_price_samples (
    symbol VARCHAR(16) NOT NULL,
    observed_at BIGINT NOT NULL,
    price_usd DECIMAL(20,8) NOT NULL,
    source VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (symbol, observed_at),
    INDEX idx_oracle_price_samples_time (observed_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
