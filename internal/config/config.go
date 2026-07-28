package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
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
const MySQLDSNEnvName = "PREDICTIONMARKET_MYSQL_DSN"
const MySQLDatabaseEnvName = "PREDICTIONMARKET_MYSQL_DATABASE"
const GoldAPIKeyEnvName = "PREDICTIONMARKET_GOLD_API_KEY"

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
	Redis struct {
		Enabled                      bool   `yaml:"enabled"`
		Address                      string `yaml:"address"`
		Password                     string `yaml:"password"`
		DB                           int    `yaml:"db"`
		KeyPrefix                    string `yaml:"key_prefix"`
		OperationTimeoutMilliseconds int    `yaml:"operation_timeout_milliseconds"`
		QuoteTTLSeconds              int    `yaml:"quote_ttl_seconds"`
		PublicDataTTLSeconds         int    `yaml:"public_data_ttl_seconds"`
		ResearchTTLSeconds           int    `yaml:"research_ttl_seconds"`
	} `yaml:"redis"`
	IPFS struct {
		Gateway          string   `yaml:"gateway"`
		FallbackGateways []string `yaml:"fallback_gateways"`
	} `yaml:"ipfs"`
	Oracle struct {
		GoldAPIURL                   string   `yaml:"gold_api_url"`
		HistoricalBaseURL            string   `yaml:"historical_base_url"`
		HistoricalAPIKey             string   `yaml:"historical_api_key"`
		BitcoinHistoricalURL         string   `yaml:"bitcoin_historical_base_url"`
		ChainlinkRPCURLs             []string `yaml:"chainlink_rpc_urls"`
		ChainlinkXAUUSDFeed          string   `yaml:"chainlink_xau_usd_feed"`
		ChainlinkBTCUSDFeed          string   `yaml:"chainlink_btc_usd_feed"`
		ChainlinkETHUSDFeed          string   `yaml:"chainlink_eth_usd_feed"`
		ChainlinkSOLUSDFeed          string   `yaml:"chainlink_sol_usd_feed"`
		ChainlinkBNBUSDFeed          string   `yaml:"chainlink_bnb_usd_feed"`
		ChainlinkPollIntervalSeconds int      `yaml:"chainlink_poll_interval_seconds"`
		ChainlinkMaxStalenessSeconds int      `yaml:"chainlink_max_staleness_seconds"`
		SinaURL                      string   `yaml:"sina_url"`
		SinaReferer                  string   `yaml:"sina_referer"`
		UserAgent                    string   `yaml:"user_agent"`
		RequestTimeoutSeconds        int      `yaml:"request_timeout_seconds"`
		SampleIntervalSeconds        int      `yaml:"sample_interval_seconds"`
	} `yaml:"oracle"`
	Sentinel struct {
		AutoResolveEnabled  *bool `yaml:"auto_resolve_enabled"`
		PollIntervalSeconds int   `yaml:"poll_interval_seconds"`
		ResolveDelaySeconds int   `yaml:"resolve_delay_seconds"`
	} `yaml:"sentinel"`
	Sampler struct {
		ChainSyncEnabled    bool `yaml:"chain_sync_enabled"`
		PollIntervalSeconds int  `yaml:"poll_interval_seconds"`
	} `yaml:"sampler"`
	AI struct {
		APIKey                  string  `yaml:"api_key"`
		BaseURL                 string  `yaml:"base_url"`
		Model                   string  `yaml:"model"`
		PollIntervalSeconds     int     `yaml:"poll_interval_seconds"`
		BuyAmountBKC            string  `yaml:"buy_amount_bkc"`
		ConfidenceMin           float64 `yaml:"confidence_min"`
		HistoryMinPoints        int     `yaml:"history_min_points"`
		HistoryMaxPoints        int     `yaml:"history_max_points"`
		KellyFraction           float64 `yaml:"kelly_fraction"`
		MinEdgePercent          float64 `yaml:"min_edge_percent"`
		MaxPositionPerMarketBKC string  `yaml:"max_position_per_market_bkc"`
		AdaptiveCooldown        bool    `yaml:"adaptive_cooldown"`
	} `yaml:"ai"`
	AIOracle struct {
		PollIntervalSeconds int `yaml:"poll_interval_seconds"`
		Consensus           struct {
			FinalArbiter      string  `yaml:"final_arbiter"`
			MinConsensusRatio float64 `yaml:"min_consensus_ratio"`
			MinConfidence     float64 `yaml:"min_confidence"`
			MinModelsRequired int     `yaml:"min_models_required"`
			TiebreakModel     string  `yaml:"tiebreak_model"`
		} `yaml:"consensus"`
		News struct {
			NewsAPIKey            string   `yaml:"news_api_key"`
			NewsAPIURL            string   `yaml:"news_api_url"`
			RSSFeeds              []string `yaml:"rss_feeds"`
			MaxArticles           int      `yaml:"max_articles"`
			LookbackHours         int      `yaml:"lookback_hours"`
			RequestTimeoutSeconds int      `yaml:"request_timeout_seconds"`
		} `yaml:"news"`
		Providers []struct {
			Name           string  `yaml:"name"`
			Model          string  `yaml:"model"`
			APIKey         string  `yaml:"api_key"`
			BaseURL        string  `yaml:"base_url"`
			Provider       string  `yaml:"provider"`
			Weight         float64 `yaml:"weight"`
			TimeoutSeconds int     `yaml:"timeout_seconds"`
		} `yaml:"providers"`
	} `yaml:"aioracle"`
}

type Config struct {
	PrivateKey                 string
	ContractAddress            string
	RPCURL                     string
	BrokerChainURL             string
	IPFSGateway                string
	IPFSFallbackGateways       []string
	GoldAPIURL                 string
	HistoricalGoldAPIBaseURL   string
	HistoricalGoldAPIKey       string
	HistoricalBitcoinBaseURL   string
	SinaURL                    string
	SinaReferer                string
	OracleUserAgent            string
	OracleRequestTimeout       time.Duration
	OracleSampleInterval       time.Duration
	ChainlinkRPCURLs           []string
	ChainlinkXAUUSDFeed        string
	ChainlinkBTCUSDFeed        string
	ChainlinkETHUSDFeed        string
	ChainlinkSOLUSDFeed        string
	ChainlinkBNBUSDFeed        string
	ChainlinkPollInterval      time.Duration
	ChainlinkMaxStaleness      time.Duration
	PollInterval               time.Duration
	ResolveDelay               time.Duration
	AutoResolveEnabled         bool
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
	AIKellyFraction            float64
	AIMinEdgePercent           float64
	AIMaxPositionPerMarketBKC  string
	AIAdaptiveCooldown         bool
	SamplerPollInterval        time.Duration
	SamplerChainSyncEnabled    bool
	MySQLDSN                   string
	MySQLMaxOpenConnections    int
	MySQLMaxIdleConnections    int
	MySQLConnectionMaxLifetime time.Duration
	RedisEnabled               bool
	RedisAddress               string
	RedisPassword              string
	RedisDB                    int
	RedisKeyPrefix             string
	RedisOperationTimeout      time.Duration
	RedisQuoteTTL              time.Duration
	RedisPublicDataTTL         time.Duration
	RedisResearchTTL           time.Duration

	// AI Oracle multi-model consensus
	AIOraclePollIntervalSeconds int
	AIOracleConsensus           ConsensusConfig
	AIOracleNews                NewsConfig
	AIOracleProviders           []ProviderConfig
}

// ConsensusConfig is exported for use by the aioracle package.
type ConsensusConfig struct {
	FinalArbiter      string
	MinConsensusRatio float64
	MinConfidence     float64
	MinModelsRequired int
	TiebreakModel     string
}

// NewsConfig is exported for use by the aioracle package.
type NewsConfig struct {
	NewsAPIKey            string
	NewsAPIURL            string
	RSSFeeds              []string
	MaxArticles           int
	LookbackHours         int
	RequestTimeoutSeconds int
}

// ProviderConfig is exported for use by the aioracle package.
type ProviderConfig struct {
	Name           string
	Model          string
	APIKey         string
	BaseURL        string
	Provider       string
	Weight         float64
	TimeoutSeconds int
}

func Load() (*Config, error) {
	slog.Info("loading config from YAML file", "file", ConfigFileName)
	cfg, err := LoadFile(ConfigFileName)
	if err != nil {
		return nil, err
	}
	if err := applyRuntimeOverrides(cfg); err != nil {
		return nil, err
	}
	if err := applyBranchProfile(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyRuntimeOverrides(cfg *Config) error {
	if key := strings.TrimSpace(os.Getenv(GoldAPIKeyEnvName)); key != "" {
		cfg.HistoricalGoldAPIKey = key
		slog.Info("using historical Gold API key from environment", "variable", GoldAPIKeyEnvName)
	}
	if dsn := strings.TrimSpace(os.Getenv(MySQLDSNEnvName)); dsn != "" {
		parsed, parseErr := mysql.ParseDSN(dsn)
		if parseErr != nil || strings.TrimSpace(parsed.DBName) == "" {
			return fmt.Errorf("%s is invalid", MySQLDSNEnvName)
		}
		cfg.MySQLDSN = dsn
		slog.Info("using MySQL DSN from environment", "variable", MySQLDSNEnvName, "database", parsed.DBName)
	}
	if database := strings.TrimSpace(os.Getenv(MySQLDatabaseEnvName)); database != "" {
		parsed, parseErr := mysql.ParseDSN(cfg.MySQLDSN)
		if parseErr != nil || strings.ContainsAny(database, "/?@") {
			return fmt.Errorf("%s is invalid", MySQLDatabaseEnvName)
		}
		parsed.DBName = database
		cfg.MySQLDSN = parsed.FormatDSN()
		slog.Info("using MySQL database from environment", "variable", MySQLDatabaseEnvName, "database", database)
	}
	return nil
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
	autoResolveEnabled := true
	if raw.Sentinel.AutoResolveEnabled != nil {
		autoResolveEnabled = *raw.Sentinel.AutoResolveEnabled
	}
	if raw.Sampler.PollIntervalSeconds <= 0 {
		return nil, errors.New("sampler.poll_interval_seconds must be positive")
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
	redisAddress := strings.TrimSpace(raw.Redis.Address)
	if redisAddress == "" {
		redisAddress = "127.0.0.1:6379"
	}
	redisKeyPrefix := strings.Trim(strings.TrimSpace(raw.Redis.KeyPrefix), ":")
	if redisKeyPrefix == "" {
		redisKeyPrefix = "predictionmarket:cn"
	}
	if raw.Redis.OperationTimeoutMilliseconds == 0 {
		raw.Redis.OperationTimeoutMilliseconds = 300
	}
	if raw.Redis.QuoteTTLSeconds == 0 {
		raw.Redis.QuoteTTLSeconds = 10
	}
	if raw.Redis.PublicDataTTLSeconds == 0 {
		raw.Redis.PublicDataTTLSeconds = 5
	}
	if raw.Redis.ResearchTTLSeconds == 0 {
		raw.Redis.ResearchTTLSeconds = 600
	}
	if raw.Redis.Enabled {
		host, port, splitErr := net.SplitHostPort(redisAddress)
		if splitErr != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
			return nil, errors.New("redis.address must use host:port format")
		}
		if raw.Redis.DB < 0 {
			return nil, errors.New("redis.db must not be negative")
		}
		if raw.Redis.OperationTimeoutMilliseconds < 50 ||
			raw.Redis.OperationTimeoutMilliseconds > 5000 {
			return nil, errors.New("redis.operation_timeout_milliseconds must be between 50 and 5000")
		}
		if raw.Redis.QuoteTTLSeconds < 1 || raw.Redis.QuoteTTLSeconds > 300 {
			return nil, errors.New("redis.quote_ttl_seconds must be between 1 and 300")
		}
		if raw.Redis.PublicDataTTLSeconds < 1 || raw.Redis.PublicDataTTLSeconds > 300 {
			return nil, errors.New("redis.public_data_ttl_seconds must be between 1 and 300")
		}
		if raw.Redis.ResearchTTLSeconds < 1 || raw.Redis.ResearchTTLSeconds > 86400 {
			return nil, errors.New("redis.research_ttl_seconds must be between 1 and 86400")
		}
	}
	if raw.Oracle.RequestTimeoutSeconds <= 0 {
		return nil, errors.New("oracle.request_timeout_seconds must be positive")
	}
	if raw.Oracle.SampleIntervalSeconds == 0 {
		raw.Oracle.SampleIntervalSeconds = 10
	}
	if raw.Oracle.SampleIntervalSeconds < 1 || raw.Oracle.SampleIntervalSeconds > 300 {
		return nil, errors.New("oracle.sample_interval_seconds must be between 1 and 300")
	}
	if len(raw.Oracle.ChainlinkRPCURLs) == 0 {
		raw.Oracle.ChainlinkRPCURLs = []string{
			"https://1rpc.io/eth",
			"https://rpc.mevblocker.io",
			"https://eth-mainnet.public.blastapi.io",
			"https://rpc.eth.gateway.fm",
			"https://eth-pokt.nodies.app",
		}
	}
	if strings.TrimSpace(raw.Oracle.ChainlinkXAUUSDFeed) == "" {
		raw.Oracle.ChainlinkXAUUSDFeed = "0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6"
	}
	if strings.TrimSpace(raw.Oracle.ChainlinkBTCUSDFeed) == "" {
		raw.Oracle.ChainlinkBTCUSDFeed = "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c"
	}
	if strings.TrimSpace(raw.Oracle.ChainlinkETHUSDFeed) == "" {
		raw.Oracle.ChainlinkETHUSDFeed = "0x5f4eC3Df9cbd43714FE2740f5E3616155c5b8419"
	}
	if strings.TrimSpace(raw.Oracle.ChainlinkSOLUSDFeed) == "" {
		raw.Oracle.ChainlinkSOLUSDFeed = "0x4ffC43a60e009B551865A93d232E33Fce9f01507"
	}
	if strings.TrimSpace(raw.Oracle.ChainlinkBNBUSDFeed) == "" {
		raw.Oracle.ChainlinkBNBUSDFeed = "0x14e613AC84a31f709eadbdF89C6CC390fDc9540A"
	}
	if raw.Oracle.ChainlinkPollIntervalSeconds == 0 {
		raw.Oracle.ChainlinkPollIntervalSeconds = 60
	}
	if raw.Oracle.ChainlinkPollIntervalSeconds < 10 || raw.Oracle.ChainlinkPollIntervalSeconds > 3600 {
		return nil, errors.New("oracle.chainlink_poll_interval_seconds must be between 10 and 3600")
	}
	if raw.Oracle.ChainlinkMaxStalenessSeconds == 0 {
		raw.Oracle.ChainlinkMaxStalenessSeconds = 43200
	}
	if raw.Oracle.ChainlinkMaxStalenessSeconds < 3600 || raw.Oracle.ChainlinkMaxStalenessSeconds > 86400 {
		return nil, errors.New("oracle.chainlink_max_staleness_seconds must be between 3600 and 86400")
	}
	if !common.IsHexAddress(raw.Oracle.ChainlinkXAUUSDFeed) {
		return nil, errors.New("oracle.chainlink_xau_usd_feed is invalid")
	}
	if !common.IsHexAddress(raw.Oracle.ChainlinkBTCUSDFeed) {
		return nil, errors.New("oracle.chainlink_btc_usd_feed is invalid")
	}
	if !common.IsHexAddress(raw.Oracle.ChainlinkETHUSDFeed) {
		return nil, errors.New("oracle.chainlink_eth_usd_feed is invalid")
	}
	if !common.IsHexAddress(raw.Oracle.ChainlinkSOLUSDFeed) {
		return nil, errors.New("oracle.chainlink_sol_usd_feed is invalid")
	}
	if !common.IsHexAddress(raw.Oracle.ChainlinkBNBUSDFeed) {
		return nil, errors.New("oracle.chainlink_bnb_usd_feed is invalid")
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
	// Kelly defaults: if not set, use safe defaults.
	if raw.AI.KellyFraction == 0 {
		raw.AI.KellyFraction = 0.25 // quarter-Kelly default
	}
	if math.IsNaN(raw.AI.KellyFraction) || raw.AI.KellyFraction <= 0 || raw.AI.KellyFraction > 1 {
		return nil, errors.New("ai.kelly_fraction must be between 0 and 1")
	}
	if raw.AI.MinEdgePercent == 0 {
		raw.AI.MinEdgePercent = 5.0 // 5% minimum edge default
	}
	if math.IsNaN(raw.AI.MinEdgePercent) || raw.AI.MinEdgePercent < 0 || raw.AI.MinEdgePercent > 100 {
		return nil, errors.New("ai.min_edge_percent must be between 0 and 100")
	}
	if raw.AI.MaxPositionPerMarketBKC == "" {
		raw.AI.MaxPositionPerMarketBKC = "50"
	}
	maxPos, err2 := strconv.ParseFloat(strings.TrimSpace(raw.AI.MaxPositionPerMarketBKC), 64)
	if err2 != nil || math.IsNaN(maxPos) || maxPos <= 0 {
		return nil, errors.New("ai.max_position_per_market_bkc must be positive")
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
	chainlinkRPCURLs := make([]string, 0, len(raw.Oracle.ChainlinkRPCURLs))
	for i, value := range raw.Oracle.ChainlinkRPCURLs {
		validated, validateErr := requireHTTPURL(fmt.Sprintf("oracle.chainlink_rpc_urls[%d]", i), value)
		if validateErr != nil {
			return nil, validateErr
		}
		chainlinkRPCURLs = append(chainlinkRPCURLs, validated)
	}
	historicalBaseURL := strings.TrimSpace(raw.Oracle.HistoricalBaseURL)
	if historicalBaseURL == "" {
		historicalBaseURL = "https://api.gold-api.com"
	}
	historicalBaseURL, err = requireHTTPURL("oracle.historical_base_url", historicalBaseURL)
	if err != nil {
		return nil, err
	}
	bitcoinHistoricalURL := strings.TrimSpace(raw.Oracle.BitcoinHistoricalURL)
	if bitcoinHistoricalURL == "" {
		bitcoinHistoricalURL = "https://api.exchange.coinbase.com"
	}
	bitcoinHistoricalURL, err = requireHTTPURL("oracle.bitcoin_historical_base_url", bitcoinHistoricalURL)
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
	ipfsFallbackGateways := make([]string, 0, len(raw.IPFS.FallbackGateways))
	for i, fallbackGateway := range raw.IPFS.FallbackGateways {
		normalized, err := requireHTTPURL(fmt.Sprintf("ipfs.fallback_gateways[%d]", i), fallbackGateway)
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(normalized, "/") {
			normalized += "/"
		}
		if normalized != ipfsGateway {
			ipfsFallbackGateways = append(ipfsFallbackGateways, normalized)
		}
	}

	// --- AI Oracle validation ---
	hasAIOracle := len(raw.AIOracle.Providers) > 0
	var oracleConsensus ConsensusConfig
	var oracleNews NewsConfig
	var oracleProviders []ProviderConfig

	if hasAIOracle {
		// Poll interval defaults.
		if raw.AIOracle.PollIntervalSeconds <= 0 {
			raw.AIOracle.PollIntervalSeconds = 300 // 5 min default
		}

		// Consensus defaults.
		if raw.AIOracle.Consensus.MinConsensusRatio == 0 {
			raw.AIOracle.Consensus.MinConsensusRatio = 0.66
		}
		if raw.AIOracle.Consensus.MinConfidence == 0 {
			raw.AIOracle.Consensus.MinConfidence = 0.60
		}
		if raw.AIOracle.Consensus.MinModelsRequired == 0 {
			raw.AIOracle.Consensus.MinModelsRequired = 1
		}
		if math.IsNaN(raw.AIOracle.Consensus.MinConsensusRatio) || raw.AIOracle.Consensus.MinConsensusRatio < 0 || raw.AIOracle.Consensus.MinConsensusRatio > 1 {
			return nil, errors.New("aioracle.consensus.min_consensus_ratio must be between 0 and 1")
		}
		if math.IsNaN(raw.AIOracle.Consensus.MinConfidence) || raw.AIOracle.Consensus.MinConfidence < 0 || raw.AIOracle.Consensus.MinConfidence > 1 {
			return nil, errors.New("aioracle.consensus.min_confidence must be between 0 and 1")
		}
		if raw.AIOracle.Consensus.MinModelsRequired < 1 {
			return nil, errors.New("aioracle.consensus.min_models_required must be at least 1")
		}

		// News defaults.
		if raw.AIOracle.News.MaxArticles == 0 {
			raw.AIOracle.News.MaxArticles = 10
		}
		if raw.AIOracle.News.LookbackHours == 0 {
			raw.AIOracle.News.LookbackHours = 72
		}
		if raw.AIOracle.News.RequestTimeoutSeconds == 0 {
			raw.AIOracle.News.RequestTimeoutSeconds = 30
		}
		if raw.AIOracle.News.NewsAPIURL != "" {
			if _, err := requireHTTPURL("aioracle.news.news_api_url", raw.AIOracle.News.NewsAPIURL); err != nil {
				return nil, err
			}
		}

		// Validate each provider.
		seenNames := make(map[string]bool)
		for i, p := range raw.AIOracle.Providers {
			if strings.TrimSpace(p.Name) == "" {
				return nil, fmt.Errorf("aioracle.providers[%d].name is required", i)
			}
			if seenNames[p.Name] {
				return nil, fmt.Errorf("aioracle.providers[%d].name %q is duplicated", i, p.Name)
			}
			seenNames[p.Name] = true

			provider := strings.ToLower(strings.TrimSpace(p.Provider))
			if !validAIOracleProvider(provider) {
				return nil, fmt.Errorf("aioracle.providers[%d].provider must be one of: deepseek, openai, anthropic, glm, minimax", i)
			}
			apiKey := strings.TrimSpace(p.APIKey)
			if isPlaceholderSecret(apiKey) {
				slog.Warn("aioracle: ignoring provider with placeholder api_key", "name", p.Name)
				continue
			}
			if apiKey == "" {
				return nil, fmt.Errorf("aioracle.providers[%d].api_key is required (provider: %s)", i, p.Name)
			}
			if p.Weight < 0 {
				return nil, fmt.Errorf("aioracle.providers[%d].weight must be non-negative", i)
			}
			if p.Weight == 0 {
				p.Weight = 1.0
			}
			if p.TimeoutSeconds == 0 {
				p.TimeoutSeconds = 60
			}

			if p.BaseURL != "" {
				if _, err := requireHTTPURL(
					fmt.Sprintf("aioracle.providers[%d].base_url", i), p.BaseURL,
				); err != nil {
					return nil, err
				}
			}

			if strings.TrimSpace(p.Model) == "" {
				return nil, fmt.Errorf("aioracle.providers[%d].model is required (provider: %s)", i, p.Name)
			}

			oracleProviders = append(oracleProviders, ProviderConfig{
				Name:           strings.TrimSpace(p.Name),
				Model:          strings.TrimSpace(p.Model),
				APIKey:         apiKey,
				BaseURL:        strings.TrimSpace(p.BaseURL),
				Provider:       provider,
				Weight:         p.Weight,
				TimeoutSeconds: p.TimeoutSeconds,
			})
		}

		if len(oracleProviders) == 0 {
			return nil, errors.New("aioracle has no usable providers after ignoring placeholder credentials")
		}
		finalArbiter := strings.TrimSpace(raw.AIOracle.Consensus.FinalArbiter)
		if finalArbiter != "" {
			if len(oracleProviders) < 2 {
				return nil, errors.New("aioracle.consensus.final_arbiter requires at least two usable providers")
			}
			found := false
			for _, provider := range oracleProviders {
				if strings.EqualFold(provider.Name, finalArbiter) {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("aioracle.consensus.final_arbiter %q does not match a usable provider", finalArbiter)
			}
		}
		if raw.AIOracle.Consensus.MinModelsRequired > len(oracleProviders) {
			return nil, fmt.Errorf(
				"aioracle.consensus.min_models_required (%d) exceeds number of providers (%d)",
				raw.AIOracle.Consensus.MinModelsRequired, len(oracleProviders),
			)
		}

		oracleConsensus = ConsensusConfig{
			FinalArbiter:      finalArbiter,
			MinConsensusRatio: raw.AIOracle.Consensus.MinConsensusRatio,
			MinConfidence:     raw.AIOracle.Consensus.MinConfidence,
			MinModelsRequired: raw.AIOracle.Consensus.MinModelsRequired,
			TiebreakModel:     strings.TrimSpace(raw.AIOracle.Consensus.TiebreakModel),
		}
		oracleNews = NewsConfig{
			NewsAPIKey:            strings.TrimSpace(raw.AIOracle.News.NewsAPIKey),
			NewsAPIURL:            strings.TrimSpace(raw.AIOracle.News.NewsAPIURL),
			RSSFeeds:              raw.AIOracle.News.RSSFeeds,
			MaxArticles:           raw.AIOracle.News.MaxArticles,
			LookbackHours:         raw.AIOracle.News.LookbackHours,
			RequestTimeoutSeconds: raw.AIOracle.News.RequestTimeoutSeconds,
		}
	}

	return &Config{
		PrivateKey:                  privateKey,
		ContractAddress:             common.HexToAddress(raw.Chain.ContractAddress).Hex(),
		RPCURL:                      rpcURL,
		BrokerChainURL:              brokerURL,
		IPFSGateway:                 ipfsGateway,
		IPFSFallbackGateways:        ipfsFallbackGateways,
		GoldAPIURL:                  goldAPIURL,
		HistoricalGoldAPIBaseURL:    strings.TrimRight(historicalBaseURL, "/"),
		HistoricalGoldAPIKey:        strings.TrimSpace(raw.Oracle.HistoricalAPIKey),
		HistoricalBitcoinBaseURL:    strings.TrimRight(bitcoinHistoricalURL, "/"),
		SinaURL:                     sinaURL,
		SinaReferer:                 sinaReferer,
		OracleUserAgent:             strings.TrimSpace(raw.Oracle.UserAgent),
		OracleRequestTimeout:        time.Duration(raw.Oracle.RequestTimeoutSeconds) * time.Second,
		OracleSampleInterval:        time.Duration(raw.Oracle.SampleIntervalSeconds) * time.Second,
		ChainlinkRPCURLs:            chainlinkRPCURLs,
		ChainlinkXAUUSDFeed:         common.HexToAddress(raw.Oracle.ChainlinkXAUUSDFeed).Hex(),
		ChainlinkBTCUSDFeed:         common.HexToAddress(raw.Oracle.ChainlinkBTCUSDFeed).Hex(),
		ChainlinkETHUSDFeed:         common.HexToAddress(raw.Oracle.ChainlinkETHUSDFeed).Hex(),
		ChainlinkSOLUSDFeed:         common.HexToAddress(raw.Oracle.ChainlinkSOLUSDFeed).Hex(),
		ChainlinkBNBUSDFeed:         common.HexToAddress(raw.Oracle.ChainlinkBNBUSDFeed).Hex(),
		ChainlinkPollInterval:       time.Duration(raw.Oracle.ChainlinkPollIntervalSeconds) * time.Second,
		ChainlinkMaxStaleness:       time.Duration(raw.Oracle.ChainlinkMaxStalenessSeconds) * time.Second,
		PollInterval:                time.Duration(raw.Sentinel.PollIntervalSeconds) * time.Second,
		ResolveDelay:                time.Duration(raw.Sentinel.ResolveDelaySeconds) * time.Second,
		AutoResolveEnabled:          autoResolveEnabled,
		UseBrokerChain:              raw.Chain.UseBrokerChain,
		HTTPListen:                  strings.TrimSpace(raw.Server.HTTPListen),
		AIAPIKey:                    apiKey,
		AIBaseURL:                   aiBaseURL,
		AIModel:                     strings.TrimSpace(raw.AI.Model),
		AIPollInterval:              time.Duration(raw.AI.PollIntervalSeconds) * time.Second,
		AIBuyAmountBKC:              strings.TrimSpace(raw.AI.BuyAmountBKC),
		AIConfidenceMin:             raw.AI.ConfidenceMin,
		AIHistoryMinPoints:          raw.AI.HistoryMinPoints,
		AIHistoryMaxPoints:          raw.AI.HistoryMaxPoints,
		AIKellyFraction:             raw.AI.KellyFraction,
		AIMinEdgePercent:            raw.AI.MinEdgePercent,
		AIMaxPositionPerMarketBKC:   strings.TrimSpace(raw.AI.MaxPositionPerMarketBKC),
		AIAdaptiveCooldown:          raw.AI.AdaptiveCooldown,
		SamplerPollInterval:         time.Duration(raw.Sampler.PollIntervalSeconds) * time.Second,
		SamplerChainSyncEnabled:     raw.Sampler.ChainSyncEnabled,
		MySQLDSN:                    mysqlDSN,
		MySQLMaxOpenConnections:     raw.MySQL.MaxOpenConnections,
		MySQLMaxIdleConnections:     raw.MySQL.MaxIdleConnections,
		MySQLConnectionMaxLifetime:  time.Duration(raw.MySQL.ConnectionMaxLifetimeSeconds) * time.Second,
		RedisEnabled:                raw.Redis.Enabled,
		RedisAddress:                redisAddress,
		RedisPassword:               raw.Redis.Password,
		RedisDB:                     raw.Redis.DB,
		RedisKeyPrefix:              redisKeyPrefix,
		RedisOperationTimeout:       time.Duration(raw.Redis.OperationTimeoutMilliseconds) * time.Millisecond,
		RedisQuoteTTL:               time.Duration(raw.Redis.QuoteTTLSeconds) * time.Second,
		RedisPublicDataTTL:          time.Duration(raw.Redis.PublicDataTTLSeconds) * time.Second,
		RedisResearchTTL:            time.Duration(raw.Redis.ResearchTTLSeconds) * time.Second,
		AIOraclePollIntervalSeconds: raw.AIOracle.PollIntervalSeconds,
		AIOracleConsensus:           oracleConsensus,
		AIOracleNews:                oracleNews,
		AIOracleProviders:           oracleProviders,
	}, nil
}

func isPlaceholderSecret(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "sk-..." ||
		normalized == "sk-ant-..." ||
		strings.HasPrefix(normalized, "replace-with-") ||
		strings.HasPrefix(normalized, "your-")
}

func validAIOracleProvider(provider string) bool {
	switch provider {
	case "deepseek", "openai", "anthropic", "glm", "minimax":
		return true
	default:
		return false
	}
}

func requireHTTPURL(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%s must be an HTTP(S) URL", field)
	}
	return value, nil
}
