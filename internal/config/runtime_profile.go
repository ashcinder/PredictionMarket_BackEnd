package config

import (
	"errors"
	"log/slog"

	mysql "github.com/go-sql-driver/mysql"
)

const (
	RuntimeProfileName = "cn-amm-supervisor"
	localSupervisorRPC = "http://127.0.0.1:42515"
	localDatabaseName  = "predictionmarket_cn_amm"
	localRedisDB       = 1
	localRedisPrefix   = "predictionmarket:cn-amm"
)

// applyBranchProfile keeps the CN market isolated while it shares the same
// immutable local Supervisor RPC with the ENG market. YAML and inherited shell
// variables may provide credentials, but cannot redirect this branch to the ENG
// cache database or the public BrokerChain endpoint.
func applyBranchProfile(cfg *Config) error {
	parsed, err := mysql.ParseDSN(cfg.MySQLDSN)
	if err != nil {
		return errors.New("apply local branch profile: invalid MySQL DSN")
	}
	parsed.DBName = localDatabaseName
	cfg.MySQLDSN = parsed.FormatDSN()
	cfg.UseBrokerChain = false
	cfg.RPCURL = localSupervisorRPC
	cfg.RedisDB = localRedisDB
	cfg.RedisKeyPrefix = localRedisPrefix

	slog.Info("prediction market runtime profile",
		"profile", RuntimeProfileName,
		"chain", localSupervisorRPC,
		"database", localDatabaseName,
		"redis_db", localRedisDB,
		"redis_prefix", localRedisPrefix,
	)
	return nil
}
