package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

const mysqlOperationTimeout = 10 * time.Second

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
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	if err := RunMigrations(pingCtx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate mysql: %w", err)
	}
	return db, nil
}

func configurePool(db *sql.DB, cfg Config) {
	db.SetMaxOpenConns(cfg.MaxOpenConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConnections)
	db.SetConnMaxLifetime(cfg.ConnectionMaxLifetime)
}
