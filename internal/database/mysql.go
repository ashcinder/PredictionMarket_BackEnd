package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

const (
	mysqlOperationTimeout = 10 * time.Second
	mysqlMigrationTimeout = 60 * time.Second

	// mySQLErrTableNotExist is the MySQL error code for "Table doesn't exist".
	mySQLErrTableNotExist = 1146
)

type Config struct {
	DSN                   string
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxLifetime time.Duration
}

func OpenMySQL(ctx context.Context, cfg Config) (*sql.DB, error) {
	parsed, err := mysql.ParseDSN(cfg.DSN)
	if err != nil {
		return nil, errors.New("parse mysql DSN: invalid configuration")
	}
	connector, err := mysql.NewConnector(parsed)
	if err != nil {
		return nil, errors.New("create mysql connector: invalid configuration")
	}
	db := sql.OpenDB(connector)
	configurePool(db, cfg)

	pingCtx, cancel := context.WithTimeout(ctx, mysqlOperationTimeout)
	if err := db.PingContext(pingCtx); err != nil {
		cancel()
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	cancel()
	migrationCtx, migrationCancel := context.WithTimeout(ctx, mysqlMigrationTimeout)
	defer migrationCancel()
	if err := RunMigrations(migrationCtx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate mysql: %w", err)
	}
	// Also ensure tables directly in case migrations were dropped.
	if err := EnsureTables(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure tables: %w", err)
	}
	return db, nil
}

func configurePool(db *sql.DB, cfg Config) {
	db.SetMaxOpenConns(cfg.MaxOpenConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConnections)
	db.SetConnMaxLifetime(cfg.ConnectionMaxLifetime)
}

// EnsureTables creates all required application tables if they do not
// already exist. It is safe to call concurrently and idempotent — every
// statement uses CREATE TABLE IF NOT EXISTS.
func EnsureTables(ctx context.Context, db *sql.DB) error {
	for _, ddl := range ensureTableDDLs {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("ensure table: %w", err)
		}
	}
	return nil
}

// IsTableNotFound reports whether err is a MySQL "table doesn't exist" error
// (code 1146). Use this to detect dropped tables and trigger recovery.
func IsTableNotFound(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == mySQLErrTableNotExist
	}
	// Also check the error chain message for the table-not-found pattern,
	// since the error may be wrapped.
	return strings.Contains(err.Error(), "Error 1146")
}

// ensureTableDDLs contains CREATE TABLE IF NOT EXISTS statements for every
// table the application needs. When adding a migration that creates or
// alters a table, update the corresponding DDL here so that EnsureTables
// always produces the latest schema.
var ensureTableDDLs = []string{
	// Tracks which migrations have been applied (also created by RunMigrations).
	`CREATE TABLE IF NOT EXISTS schema_migrations (
	version BIGINT NOT NULL PRIMARY KEY,
	applied_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Market history chart data (final schema after migration 002).
	`CREATE TABLE IF NOT EXISTS market_history (
	contract_address VARCHAR(42) NOT NULL,
	game_id BIGINT UNSIGNED NOT NULL,
	observed_at BIGINT UNSIGNED NOT NULL,
	yes_percent DECIMAL(9,6) NOT NULL,
	no_percent DECIMAL(9,6) NOT NULL,
	reserve_no VARBINARY(80) NULL,
	reserve_yes VARBINARY(80) NULL,
	source VARCHAR(16) NOT NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	PRIMARY KEY (contract_address, game_id, observed_at),
	INDEX idx_market_history_latest (contract_address, game_id, observed_at DESC),
	CONSTRAINT chk_market_history_source CHECK (source IN ('chain','ipfs')),
	CONSTRAINT chk_market_history_percent CHECK (
		yes_percent BETWEEN 0 AND 100 AND no_percent BETWEEN 0 AND 100
	)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// AI decision audit trail.
	`CREATE TABLE IF NOT EXISTS ai_decisions (
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
		'low_confidence','cooldown','traded','trade_failed',
		'sync_failed','sync_cooldown','metadata_unavailable','quote_unavailable'
	))
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Per-market sync health for AI-managed polling backoff.
	`CREATE TABLE IF NOT EXISTS market_sync_state (
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
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Persistent AI-managed entries. Private keys are encrypted by the
	// application before being stored in key_ciphertext.
	`CREATE TABLE IF NOT EXISTS ai_managed_entries (
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
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Gold game metadata cache (DApp v1 API).
	`CREATE TABLE IF NOT EXISTS gold_games (
	game_id INTEGER NOT NULL,
	contract_address VARCHAR(42) NOT NULL,
	ipfs_cid VARCHAR(128) NOT NULL,
	` + "`desc`" + ` TEXT,
	` + "`condition`" + ` TEXT,
	avatar_url VARCHAR(256) DEFAULT '',
	detailed_info TEXT,
	option_yes VARCHAR(64) DEFAULT 'YES',
	option_no VARCHAR(64) DEFAULT 'NO',
	creator_address VARCHAR(42) DEFAULT '',
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	PRIMARY KEY (game_id),
	INDEX idx_gold_games_cid (ipfs_cid)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Chain state cache (DApp v1 API).
	`CREATE TABLE IF NOT EXISTS gold_chain_states (
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
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// User positions per game (DApp v1 API).
	`CREATE TABLE IF NOT EXISTS gold_user_positions (
	user_address VARCHAR(42) NOT NULL,
	game_id INTEGER NOT NULL,
	my_shares_yes VARBINARY(80) NULL,
	my_shares_no VARBINARY(80) NULL,
	updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
	PRIMARY KEY (user_address, game_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Price history chart data (DApp v1 API).
	`CREATE TABLE IF NOT EXISTS gold_price_history (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
	game_id INTEGER NOT NULL,
	timestamp_sec BIGINT NOT NULL,
	yes_price DECIMAL(9,6) NOT NULL,
	no_price DECIMAL(9,6) NOT NULL,
	total_pool VARBINARY(80) NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	PRIMARY KEY (id),
	UNIQUE KEY uq_gold_price_history_game_time (game_id, timestamp_sec),
	INDEX idx_history_game_time (game_id, timestamp_sec DESC)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Trade records (DApp v1 API).
	`CREATE TABLE IF NOT EXISTS gold_trades (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
	game_id INTEGER NOT NULL,
	contract_address VARCHAR(42) NOT NULL,
	user_address VARCHAR(42) NOT NULL,
	trade_type VARCHAR(10) NOT NULL,
	option_id TINYINT NOT NULL DEFAULT 0,
	amount_wei VARBINARY(80) NULL,
	share_amount_wei VARCHAR(78) NOT NULL DEFAULT '0',
	shares_wei VARBINARY(80) NULL,
	price_at_trade DOUBLE NULL,
	timestamp_sec BIGINT NOT NULL DEFAULT 0,
	tx_hash VARCHAR(80) NOT NULL DEFAULT '',
	is_success TINYINT(1) NOT NULL DEFAULT 0,
	is_ai_managed TINYINT(1) NOT NULL DEFAULT 0,
	my_shares_yes_after VARCHAR(78) NULL,
	my_shares_no_after VARCHAR(78) NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	PRIMARY KEY (id),
	INDEX idx_trades_game (game_id, created_at DESC),
	INDEX idx_trades_user (user_address, created_at DESC),
	INDEX idx_trades_game_user_time (game_id, user_address, timestamp_sec DESC),
	INDEX idx_gold_trades_game_user (game_id, user_address)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}
