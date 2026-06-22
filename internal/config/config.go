package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	mysql "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

const ConfigFileName = "config.yaml"

type fileConfig struct {
	Chain struct {
		PrivateKey      string `yaml:"private_key"`
		ContractAddress string `yaml:"contract_address"`
		RPCURL          string `yaml:"rpc_url"`
		BrokerChainURL  string `yaml:"broker_chain_url"`
		UseBrokerChain  bool   `yaml:"use_broker_chain"`
	} `yaml:"chain"`
	Server struct {
		HTTPListen string `yaml:"http_listen"`
	} `yaml:"server"`
	MySQL struct {
		DSN                          string `yaml:"dsn"`
		MaxOpenConnections           int    `yaml:"max_open_connections"`
		MaxIdleConnections           int    `yaml:"max_idle_connections"`
		ConnectionMaxLifetimeSeconds int    `yaml:"connection_max_lifetime_seconds"`
	} `yaml:"mysql"`
	IPFS struct {
		Gateway string `yaml:"gateway"`
	} `yaml:"ipfs"`
	Oracle struct {
		GoldAPIURL            string `yaml:"gold_api_url"`
		SinaURL               string `yaml:"sina_url"`
		SinaReferer           string `yaml:"sina_referer"`
		UserAgent             string `yaml:"user_agent"`
		RequestTimeoutSeconds int    `yaml:"request_timeout_seconds"`
	} `yaml:"oracle"`
	Sentinel struct {
		PollIntervalSeconds int `yaml:"poll_interval_seconds"`
		ResolveDelaySeconds int `yaml:"resolve_delay_seconds"`
	} `yaml:"sentinel"`
	AI struct {
		APIKey              string  `yaml:"api_key"`
		BaseURL             string  `yaml:"base_url"`
		Model               string  `yaml:"model"`
		PollIntervalSeconds int     `yaml:"poll_interval_seconds"`
		BuyAmountBKC        string  `yaml:"buy_amount_bkc"`
		ConfidenceMin       float64 `yaml:"confidence_min"`
		HistoryMinPoints    int     `yaml:"history_min_points"`
		HistoryMaxPoints    int     `yaml:"history_max_points"`
	} `yaml:"ai"`
}

type Config struct {
	PrivateKey                 string
	ContractAddress            string
	RPCURL                     string
	BrokerChainURL             string
	IPFSGateway                string
	GoldAPIURL                 string
	SinaURL                    string
	SinaReferer                string
	OracleUserAgent            string
	OracleRequestTimeout       time.Duration
	PollInterval               time.Duration
	ResolveDelay               time.Duration
	UseBrokerChain             bool
	HTTPListen                 string
	AIAPIKey                   string
	AIBaseURL                  string
	AIModel                    string
	AIPollInterval             time.Duration
	AIBuyAmountBKC             string
	AIConfidenceMin            float64
	AIHistoryMinPoints         int
	AIHistoryMaxPoints         int
	MySQLDSN                   string
	MySQLMaxOpenConnections    int
	MySQLMaxIdleConnections    int
	MySQLConnectionMaxLifetime time.Duration
}

func Load() (*Config, error) {
	slog.Info("loading config from YAML file", "file", ConfigFileName)
	return LoadFile(ConfigFileName)
}

func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var raw fileConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse YAML config %s: %w", path, err)
	}

	privateKey := strings.TrimSpace(raw.Chain.PrivateKey)
	if privateKey == "" || strings.HasPrefix(privateKey, "replace-with-") {
		return nil, errors.New("chain.private_key is required")
	}
	if _, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x")); err != nil {
		return nil, errors.New("chain.private_key is invalid")
	}
	if !common.IsHexAddress(raw.Chain.ContractAddress) {
		return nil, errors.New("chain.contract_address is invalid")
	}

	apiKey := strings.TrimSpace(raw.AI.APIKey)
	if apiKey == "" || strings.HasPrefix(apiKey, "replace-with-") {
		return nil, errors.New("ai.api_key is required")
	}
	if strings.TrimSpace(raw.AI.Model) == "" {
		return nil, errors.New("ai.model is required")
	}
	if strings.TrimSpace(raw.Server.HTTPListen) == "" {
		return nil, errors.New("server.http_listen is required")
	}
	if raw.Sentinel.PollIntervalSeconds <= 0 {
		return nil, errors.New("sentinel.poll_interval_seconds must be positive")
	}
	if raw.Sentinel.ResolveDelaySeconds < 0 {
		return nil, errors.New("sentinel.resolve_delay_seconds must not be negative")
	}
	if raw.AI.PollIntervalSeconds <= 0 {
		return nil, errors.New("ai.poll_interval_seconds must be positive")
	}
	if raw.AI.HistoryMinPoints == 0 {
		raw.AI.HistoryMinPoints = 3
	}
	if raw.AI.HistoryMinPoints < 0 {
		return nil, errors.New("ai.history_min_points must be positive")
	}
	if raw.AI.HistoryMaxPoints == 0 {
		raw.AI.HistoryMaxPoints = 256
	}
	if raw.AI.HistoryMaxPoints < 0 {
		return nil, errors.New("ai.history_max_points must be positive")
	}
	if raw.AI.HistoryMaxPoints < raw.AI.HistoryMinPoints {
		return nil, errors.New("ai.history_max_points must be greater than or equal to ai.history_min_points")
	}
	if raw.AI.HistoryMaxPoints > 1000 {
		return nil, errors.New("ai.history_max_points must not exceed 1000")
	}
	mysqlDSN := strings.TrimSpace(raw.MySQL.DSN)
	if mysqlDSN == "" || strings.Contains(mysqlDSN, "replace-with-") {
		return nil, errors.New("mysql.dsn is required")
	}
	parsedMySQLDSN, err := mysql.ParseDSN(mysqlDSN)
	if err != nil || strings.TrimSpace(parsedMySQLDSN.DBName) == "" {
		return nil, errors.New("mysql.dsn is invalid")
	}
	if raw.MySQL.MaxOpenConnections <= 0 {
		return nil, errors.New("mysql.max_open_connections must be positive")
	}
	if raw.MySQL.MaxIdleConnections <= 0 {
		return nil, errors.New("mysql.max_idle_connections must be positive")
	}
	if raw.MySQL.MaxIdleConnections > raw.MySQL.MaxOpenConnections {
		return nil, errors.New("mysql.max_idle_connections must not exceed mysql.max_open_connections")
	}
	if raw.MySQL.ConnectionMaxLifetimeSeconds <= 0 {
		return nil, errors.New("mysql.connection_max_lifetime_seconds must be positive")
	}
	if raw.Oracle.RequestTimeoutSeconds <= 0 {
		return nil, errors.New("oracle.request_timeout_seconds must be positive")
	}
	if strings.TrimSpace(raw.Oracle.UserAgent) == "" {
		return nil, errors.New("oracle.user_agent is required")
	}
	if math.IsNaN(raw.AI.ConfidenceMin) || math.IsInf(raw.AI.ConfidenceMin, 0) ||
		raw.AI.ConfidenceMin < 0 || raw.AI.ConfidenceMin > 1 {
		return nil, errors.New("ai.confidence_min must be between 0 and 1")
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(raw.AI.BuyAmountBKC), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return nil, errors.New("ai.buy_amount_bkc must be positive")
	}

	brokerURL, err := requireHTTPURL("chain.broker_chain_url", raw.Chain.BrokerChainURL)
	if err != nil {
		return nil, err
	}
	rpcURL := strings.TrimSpace(raw.Chain.RPCURL)
	if !raw.Chain.UseBrokerChain {
		if rpcURL, err = requireHTTPURL("chain.rpc_url", rpcURL); err != nil {
			return nil, err
		}
	} else if rpcURL != "" {
		if rpcURL, err = requireHTTPURL("chain.rpc_url", rpcURL); err != nil {
			return nil, err
		}
	}
	ipfsGateway, err := requireHTTPURL("ipfs.gateway", raw.IPFS.Gateway)
	if err != nil {
		return nil, err
	}
	aiBaseURL, err := requireHTTPURL("ai.base_url", raw.AI.BaseURL)
	if err != nil {
		return nil, err
	}
	goldAPIURL, err := requireHTTPURL("oracle.gold_api_url", raw.Oracle.GoldAPIURL)
	if err != nil {
		return nil, err
	}
	sinaURL, err := requireHTTPURL("oracle.sina_url", raw.Oracle.SinaURL)
	if err != nil {
		return nil, err
	}
	sinaReferer, err := requireHTTPURL("oracle.sina_referer", raw.Oracle.SinaReferer)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(ipfsGateway, "/") {
		ipfsGateway += "/"
	}

	return &Config{
		PrivateKey:                 privateKey,
		ContractAddress:            common.HexToAddress(raw.Chain.ContractAddress).Hex(),
		RPCURL:                     rpcURL,
		BrokerChainURL:             brokerURL,
		IPFSGateway:                ipfsGateway,
		GoldAPIURL:                 goldAPIURL,
		SinaURL:                    sinaURL,
		SinaReferer:                sinaReferer,
		OracleUserAgent:            strings.TrimSpace(raw.Oracle.UserAgent),
		OracleRequestTimeout:       time.Duration(raw.Oracle.RequestTimeoutSeconds) * time.Second,
		PollInterval:               time.Duration(raw.Sentinel.PollIntervalSeconds) * time.Second,
		ResolveDelay:               time.Duration(raw.Sentinel.ResolveDelaySeconds) * time.Second,
		UseBrokerChain:             raw.Chain.UseBrokerChain,
		HTTPListen:                 strings.TrimSpace(raw.Server.HTTPListen),
		AIAPIKey:                   apiKey,
		AIBaseURL:                  aiBaseURL,
		AIModel:                    strings.TrimSpace(raw.AI.Model),
		AIPollInterval:             time.Duration(raw.AI.PollIntervalSeconds) * time.Second,
		AIBuyAmountBKC:             strings.TrimSpace(raw.AI.BuyAmountBKC),
		AIConfidenceMin:            raw.AI.ConfidenceMin,
		AIHistoryMinPoints:         raw.AI.HistoryMinPoints,
		AIHistoryMaxPoints:         raw.AI.HistoryMaxPoints,
		MySQLDSN:                   mysqlDSN,
		MySQLMaxOpenConnections:    raw.MySQL.MaxOpenConnections,
		MySQLMaxIdleConnections:    raw.MySQL.MaxIdleConnections,
		MySQLConnectionMaxLifetime: time.Duration(raw.MySQL.ConnectionMaxLifetimeSeconds) * time.Second,
	}, nil
}

func requireHTTPURL(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%s must be an HTTP(S) URL", field)
	}
	return value, nil
}
