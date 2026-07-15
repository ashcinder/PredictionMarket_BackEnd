// Command clear-local-data removes application data from the configured MySQL
// database while preserving all table definitions and schema_migrations.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"PredictionMarket/internal/config"
	_ "github.com/go-sql-driver/mysql"
)

var dataTables = []string{
	"gold_trades",
	"gold_price_history",
	"gold_portfolio_history",
	"gold_user_positions",
	"gold_chain_states",
	"gold_games",
	"ai_managed_entries",
	"ai_decisions",
	"market_sync_state",
	"market_history",
	"oracle_price_samples",
	"oracle_chainlink_rounds",
}

func main() {
	configPath := flag.String("config", "config.yaml", "backend YAML configuration")
	flag.Parse()
	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fatal(err)
	}
	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		fatal(fmt.Errorf("open database: %w", err))
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fatal(fmt.Errorf("connect database: %w", err))
	}
	if _, err := db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		fatal(fmt.Errorf("disable foreign key checks: %w", err))
	}
	defer db.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS = 1")
	for _, table := range dataTables {
		var exists int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", table,
		).Scan(&exists); err != nil {
			fatal(fmt.Errorf("check table %s: %w", table, err))
		}
		if exists == 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, "TRUNCATE TABLE `"+table+"`"); err != nil {
			fatal(fmt.Errorf("clear %s: %w", table, err))
		}
		var remaining int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&remaining); err != nil {
			fatal(fmt.Errorf("verify %s: %w", table, err))
		}
		if remaining != 0 {
			fatal(fmt.Errorf("verify %s: expected 0 rows, found %d", table, remaining))
		}
		fmt.Printf("cleared=%s remaining=0\n", table)
	}
	fmt.Println("database_data_reset=complete")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "clear-local-data:", err)
	os.Exit(1)
}
