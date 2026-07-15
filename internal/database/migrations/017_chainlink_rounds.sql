CREATE TABLE IF NOT EXISTS oracle_chainlink_rounds (
    feed_address VARCHAR(42) NOT NULL,
    round_id DECIMAL(30,0) NOT NULL,
    answer_raw VARCHAR(80) NOT NULL,
    decimals TINYINT UNSIGNED NOT NULL,
    price_usd DECIMAL(30,8) NOT NULL,
    started_at_sec BIGINT NOT NULL,
    updated_at_sec BIGINT NOT NULL,
    answered_in_round DECIMAL(30,0) NOT NULL,
    fetched_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (feed_address, round_id),
    INDEX idx_chainlink_feed_time (feed_address, updated_at_sec DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
