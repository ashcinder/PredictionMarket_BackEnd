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
	} `yaml:"ai"`
}

type Config struct {
	PrivateKey           string
	ContractAddress      string
	RPCURL               string
	BrokerChainURL       string
	IPFSGateway          string
	GoldAPIURL           string
	SinaURL              string
	SinaReferer          string
	OracleUserAgent      string
	OracleRequestTimeout time.Duration
	PollInterval         time.Duration
	ResolveDelay         time.Duration
	UseBrokerChain       bool
	HTTPListen           string
	AIAPIKey             string
	AIBaseURL            string
	AIModel              string
	AIPollInterval       time.Duration
	AIBuyAmountBKC       string
	AIConfidenceMin      float64
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
		PrivateKey:           privateKey,
		ContractAddress:      common.HexToAddress(raw.Chain.ContractAddress).Hex(),
		RPCURL:               rpcURL,
		BrokerChainURL:       brokerURL,
		IPFSGateway:          ipfsGateway,
		GoldAPIURL:           goldAPIURL,
		SinaURL:              sinaURL,
		SinaReferer:          sinaReferer,
		OracleUserAgent:      strings.TrimSpace(raw.Oracle.UserAgent),
		OracleRequestTimeout: time.Duration(raw.Oracle.RequestTimeoutSeconds) * time.Second,
		PollInterval:         time.Duration(raw.Sentinel.PollIntervalSeconds) * time.Second,
		ResolveDelay:         time.Duration(raw.Sentinel.ResolveDelaySeconds) * time.Second,
		UseBrokerChain:       raw.Chain.UseBrokerChain,
		HTTPListen:           strings.TrimSpace(raw.Server.HTTPListen),
		AIAPIKey:             apiKey,
		AIBaseURL:            aiBaseURL,
		AIModel:              strings.TrimSpace(raw.AI.Model),
		AIPollInterval:       time.Duration(raw.AI.PollIntervalSeconds) * time.Second,
		AIBuyAmountBKC:       strings.TrimSpace(raw.AI.BuyAmountBKC),
		AIConfidenceMin:      raw.AI.ConfidenceMin,
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
