package config

import (
	"errors"
	"log/slog"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

const (
	RuntimeProfileName   = "agent-oracle-remote"
	remoteBrokerChainURL = "https://dash.broker-chain.com:443/"
	remoteDatabaseName   = "brokerchain_db"
)

// applyBranchProfile changes only the two branch-specific connection targets.
// All polling, AI management, API, persistence and fallback behavior remains
// shared with the local Supervisor branch.
func applyBranchProfile(cfg *Config) error {
	parsed, err := mysql.ParseDSN(cfg.MySQLDSN)
	if err != nil {
		return errors.New("apply remote branch profile: invalid MySQL DSN")
	}
	if strings.TrimSpace(parsed.User) == "" {
		return errors.New("apply remote branch profile: MySQL user is required")
	}

	parsed.DBName = remoteDatabaseName
	cfg.MySQLDSN = parsed.FormatDSN()
	cfg.UseBrokerChain = true
	cfg.BrokerChainURL = remoteBrokerChainURL
	cfg.RPCURL = ""

	slog.Info("prediction market runtime profile",
		"profile", RuntimeProfileName,
		"chain", remoteBrokerChainURL,
		"database", remoteDatabaseName,
	)
	return nil
}
