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
		'low_confidence','cooldown','traded','trade_failed'
	))
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// Game cache (chain data snapshot for fast API reads).
	`CREATE TABLE IF NOT EXISTS gold_games (
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
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

	// User positions (shares per user per game).
	`CREATE TABLE IF NOT EXISTS gold_user_positions (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		game_id BIGINT UNSIGNED NOT NULL,
		user_address VARCHAR(42) NOT NULL,
		shares_yes DECIMAL(65,0) NOT NULL DEFAULT 0,
		shares_no DECIMAL(65,0) NOT NULL DEFAULT 0,
		updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
		PRIMARY KEY (id),
		UNIQUE KEY uk_game_user (game_id, user_address),
		INDEX idx_user_address (user_address)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}
