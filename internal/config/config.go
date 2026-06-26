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
	Sampler struct {
		PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	} `yaml:"sampler"`
	AI struct {
		APIKey              string  `yaml:"api_key"`
		BaseURL             string  `yaml:"base_url"`
		Model               string  `yaml:"model"`
		PollIntervalSeconds int     `yaml:"poll_interval_seconds"`
		BuyAmountBKC        string  `yaml:"buy_amount_bkc"`
		ConfidenceMin       float64 `yaml:"confidence_min"`
		HistoryMinPoints       int     `yaml:"history_min_points"`
		HistoryMaxPoints       int     `yaml:"history_max_points"`
		KellyFraction          float64 `yaml:"kelly_fraction"`
		MinEdgePercent         float64 `yaml:"min_edge_percent"`
		MaxPositionPerMarketBKC string  `yaml:"max_position_per_market_bkc"`
		AdaptiveCooldown       bool    `yaml:"adaptive_cooldown"`
	} `yaml:"ai"`
	AIOracle struct {
		PollIntervalSeconds int `yaml:"poll_interval_seconds"`
		Consensus           struct {
			MinConsensusRatio float64 `yaml:"min_consensus_ratio"`
			MinConfidence     float64 `yaml:"min_confidence"`
			MinModelsRequired int     `yaml:"min_models_required"`
			TiebreakModel     string  `yaml:"tiebreak_model"`
		} `yaml:"consensus"`
		News struct {
			NewsAPIKey             string   `yaml:"news_api_key"`
			NewsAPIURL             string   `yaml:"news_api_url"`
			RSSFeeds               []string `yaml:"rss_feeds"`
			MaxArticles            int      `yaml:"max_articles"`
			LookbackHours          int      `yaml:"lookback_hours"`
			RequestTimeoutSeconds  int      `yaml:"request_timeout_seconds"`
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
	AIKellyFraction            float64
	AIMinEdgePercent           float64
	AIMaxPositionPerMarketBKC  string
	AIAdaptiveCooldown         bool
	SamplerPollInterval         time.Duration
	MySQLDSN                   string
	MySQLMaxOpenConnections    int
	MySQLMaxIdleConnections    int
	MySQLConnectionMaxLifetime time.Duration

	// AI Oracle multi-model consensus
	AIOraclePollIntervalSeconds   int
	AIOracleConsensus             ConsensusConfig
	AIOracleNews                  NewsConfig
	AIOracleProviders             []ProviderConfig
}

// ConsensusConfig is exported for use by the aioracle package.
type ConsensusConfig struct {
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
			if provider != "deepseek" && provider != "openai" && provider != "anthropic" {
				return nil, fmt.Errorf("aioracle.providers[%d].provider must be one of: deepseek, openai, anthropic", i)
			}
			if strings.TrimSpace(p.APIKey) == "" {
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
				APIKey:         strings.TrimSpace(p.APIKey),
				BaseURL:        strings.TrimSpace(p.BaseURL),
				Provider:       provider,
				Weight:         p.Weight,
				TimeoutSeconds: p.TimeoutSeconds,
			})
		}

		if raw.AIOracle.Consensus.MinModelsRequired > len(oracleProviders) {
			return nil, fmt.Errorf(
				"aioracle.consensus.min_models_required (%d) exceeds number of providers (%d)",
				raw.AIOracle.Consensus.MinModelsRequired, len(oracleProviders),
			)
		}

		oracleConsensus = ConsensusConfig{
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
		AIKellyFraction:            raw.AI.KellyFraction,
		AIMinEdgePercent:           raw.AI.MinEdgePercent,
		AIMaxPositionPerMarketBKC:  strings.TrimSpace(raw.AI.MaxPositionPerMarketBKC),
		AIAdaptiveCooldown:         raw.AI.AdaptiveCooldown,
		SamplerPollInterval:        time.Duration(raw.Sampler.PollIntervalSeconds) * time.Second,
		MySQLDSN:                   mysqlDSN,
		MySQLMaxOpenConnections:    raw.MySQL.MaxOpenConnections,
		MySQLMaxIdleConnections:    raw.MySQL.MaxIdleConnections,
		MySQLConnectionMaxLifetime: time.Duration(raw.MySQL.ConnectionMaxLifetimeSeconds) * time.Second,
		AIOraclePollIntervalSeconds:   raw.AIOracle.PollIntervalSeconds,
		AIOracleConsensus:             oracleConsensus,
		AIOracleNews:                  oracleNews,
		AIOracleProviders:             oracleProviders,
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
