package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"predictionmarket-simulator/internal/amount"
	"predictionmarket-simulator/internal/scenario"

	"github.com/ethereum/go-ethereum/common"
	mysql "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

const (
	ScenarioCreateAndTrade = "create_and_trade"
	ScenarioTradeExisting  = "trade_existing"

	ModePreview = "preview"
	ModeExecute = "execute"
)

type rawConfig struct {
	Runtime struct {
		Enabled        bool   `yaml:"enabled"`
		Mode           string `yaml:"mode"`
		OnChain        bool   `yaml:"on_chain"`
		DryRun         bool   `yaml:"dry_run"`
		PlanFile       string `yaml:"plan_file"`
		ApproveOnChain bool   `yaml:"approve_on_chain"`
	} `yaml:"runtime"`
	Chain struct {
		PrivateKey      string `yaml:"private_key"`
		ContractAddress string `yaml:"contract_address"`
		RPCURL          string `yaml:"rpc_url"`
		BrokerChainURL  string `yaml:"broker_chain_url"`
		UseBrokerChain  bool   `yaml:"use_broker_chain"`
	} `yaml:"chain"`
	MySQL struct {
		DSN                          string `yaml:"dsn"`
		MaxOpenConnections           int    `yaml:"max_open_connections"`
		MaxIdleConnections           int    `yaml:"max_idle_connections"`
		ConnectionMaxLifetimeSeconds int    `yaml:"connection_max_lifetime_seconds"`
	} `yaml:"mysql"`
	Scenario struct {
		Type               string `yaml:"type"`
		MarketCount        int    `yaml:"market_count"`
		ExistingGameIDs    []int  `yaml:"existing_game_ids"`
		Participants       int    `yaml:"participants"`
		TradesPerMarketMin int    `yaml:"trades_per_market_min"`
		TradesPerMarketMax int    `yaml:"trades_per_market_max"`
	} `yaml:"scenario"`
	Market struct {
		Types                  []string `yaml:"types"`
		InitialLiquidityMinBKC string   `yaml:"initial_liquidity_min_bkc"`
		InitialLiquidityMaxBKC string   `yaml:"initial_liquidity_max_bkc"`
		DurationMinSeconds     int64    `yaml:"duration_min_seconds"`
		DurationMaxSeconds     int64    `yaml:"duration_max_seconds"`
	} `yaml:"market"`
	Trade struct {
		BuyMinBKC         string `yaml:"buy_min_bkc"`
		BuyMaxBKC         string `yaml:"buy_max_bkc"`
		CreatorAlsoTrades bool   `yaml:"creator_also_trades"`
	} `yaml:"trade"`
	Timing struct {
		PauseSeconds   float64 `yaml:"pause_seconds"`
		TimeoutSeconds int     `yaml:"timeout_seconds"`
	} `yaml:"timing"`
}

type Config struct {
	Runtime  RuntimeConfig
	Chain    ChainConfig
	MySQL    MySQLConfig
	Scenario ScenarioConfig
	Market   MarketConfig
	Trade    TradeConfig
	Timing   TimingConfig
}

type RuntimeConfig struct {
	Enabled        bool
	Mode           string
	OnChain        bool
	DryRun         bool
	PlanFile       string
	ApproveOnChain bool
}

type ChainConfig struct {
	PrivateKey      string
	ContractAddress string
	RPCURL          string
	BrokerChainURL  string
	UseBrokerChain  bool
}

type MySQLConfig struct {
	DSN                   string
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxLifetime time.Duration
}

type ScenarioConfig struct {
	Type               string
	MarketCount        int
	ExistingGameIDs    []int
	Participants       int
	TradesPerMarketMin int
	TradesPerMarketMax int
}

type MarketConfig struct {
	Types                  []string
	InitialLiquidityMinBKC string
	InitialLiquidityMaxBKC string
	DurationMin            time.Duration
	DurationMax            time.Duration
}

type TradeConfig struct {
	BuyMinBKC         string
	BuyMaxBKC         string
	CreatorAlsoTrades bool
}

type TimingConfig struct {
	Pause   time.Duration
	Timeout time.Duration
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyDefaults(&raw)
	if err := validate(&raw); err != nil {
		return nil, err
	}
	return buildConfig(&raw), nil
}

func applyDefaults(raw *rawConfig) {
	if strings.TrimSpace(raw.Runtime.Mode) == "" {
		if raw.Runtime.DryRun {
			raw.Runtime.Mode = ModePreview
		} else {
			raw.Runtime.Mode = ModeExecute
		}
	}
	if strings.TrimSpace(raw.Runtime.PlanFile) == "" {
		raw.Runtime.PlanFile = "out/simulator-plan.json"
	}
	if strings.TrimSpace(raw.Scenario.Type) == "" {
		raw.Scenario.Type = ScenarioCreateAndTrade
	}
	if raw.Scenario.MarketCount <= 0 {
		raw.Scenario.MarketCount = 1
	}
	if raw.Scenario.Participants <= 0 {
		raw.Scenario.Participants = 6
	}
	if raw.Scenario.TradesPerMarketMin <= 0 {
		raw.Scenario.TradesPerMarketMin = 4
	}
	if raw.Scenario.TradesPerMarketMax <= 0 {
		raw.Scenario.TradesPerMarketMax = 12
	}
	if len(raw.Market.Types) == 0 {
		raw.Market.Types = scenario.SupportedTypes()
	}
	if strings.TrimSpace(raw.Market.InitialLiquidityMinBKC) == "" {
		raw.Market.InitialLiquidityMinBKC = "3"
	}
	if strings.TrimSpace(raw.Market.InitialLiquidityMaxBKC) == "" {
		raw.Market.InitialLiquidityMaxBKC = "12"
	}
	if raw.Market.DurationMinSeconds <= 0 {
		raw.Market.DurationMinSeconds = 3600
	}
	if raw.Market.DurationMaxSeconds <= 0 {
		raw.Market.DurationMaxSeconds = 86400
	}
	if strings.TrimSpace(raw.Trade.BuyMinBKC) == "" {
		raw.Trade.BuyMinBKC = "0.2"
	}
	if strings.TrimSpace(raw.Trade.BuyMaxBKC) == "" {
		raw.Trade.BuyMaxBKC = "2"
	}
	if raw.Timing.TimeoutSeconds <= 0 {
		raw.Timing.TimeoutSeconds = 120
	}
	if raw.MySQL.MaxOpenConnections <= 0 {
		raw.MySQL.MaxOpenConnections = 10
	}
	if raw.MySQL.MaxIdleConnections <= 0 {
		raw.MySQL.MaxIdleConnections = 2
	}
	if raw.MySQL.ConnectionMaxLifetimeSeconds <= 0 {
		raw.MySQL.ConnectionMaxLifetimeSeconds = 300
	}
}

func validate(raw *rawConfig) error {
	switch raw.Runtime.Mode {
	case ModePreview, ModeExecute:
	default:
		return fmt.Errorf("runtime.mode must be %q or %q", ModePreview, ModeExecute)
	}
	if raw.Runtime.Mode == ModeExecute && raw.Runtime.DryRun {
		return errors.New("runtime.dry_run cannot be true when runtime.mode is execute")
	}
	if strings.TrimSpace(raw.Runtime.PlanFile) == "" {
		return errors.New("runtime.plan_file is required")
	}
	switch raw.Scenario.Type {
	case ScenarioCreateAndTrade, ScenarioTradeExisting:
	default:
		return fmt.Errorf("scenario.type must be %q or %q", ScenarioCreateAndTrade, ScenarioTradeExisting)
	}
	if raw.Scenario.TradesPerMarketMin > raw.Scenario.TradesPerMarketMax {
		return errors.New("scenario.trades_per_market_min must be <= trades_per_market_max")
	}
	if raw.Scenario.Type == ScenarioTradeExisting && !raw.Runtime.OnChain && raw.Runtime.Mode == ModeExecute {
		return errors.New("scenario.type=trade_existing requires runtime.on_chain=true when runtime.mode is execute")
	}
	if raw.Scenario.Participants < 2 && !raw.Trade.CreatorAlsoTrades {
		return errors.New("scenario.participants must be at least 2 when trade.creator_also_trades is false")
	}
	if raw.Market.DurationMinSeconds > raw.Market.DurationMaxSeconds {
		return errors.New("market.duration_min_seconds must be <= duration_max_seconds")
	}
	if err := validateBKCAmountRange("market.initial_liquidity", raw.Market.InitialLiquidityMinBKC, raw.Market.InitialLiquidityMaxBKC); err != nil {
		return err
	}
	if err := validateBKCAmountRange("trade.buy", raw.Trade.BuyMinBKC, raw.Trade.BuyMaxBKC); err != nil {
		return err
	}
	for _, typ := range raw.Market.Types {
		if _, ok := scenario.TemplateForType(typ); !ok {
			return fmt.Errorf("unsupported market type %q", typ)
		}
	}
	if !common.IsHexAddress(raw.Chain.ContractAddress) {
		return errors.New("chain.contract_address is invalid")
	}
	if raw.Runtime.Mode == ModeExecute && raw.Runtime.OnChain {
		if !raw.Runtime.ApproveOnChain {
			return errors.New("runtime.approve_on_chain must be true to execute on-chain transactions")
		}
		if strings.TrimSpace(raw.Chain.PrivateKey) == "" || strings.HasPrefix(raw.Chain.PrivateKey, "replace-with-") {
			return errors.New("chain.private_key is required when executing on-chain transactions")
		}
		if raw.Chain.UseBrokerChain {
			if strings.TrimSpace(raw.Chain.BrokerChainURL) == "" {
				return errors.New("chain.broker_chain_url is required when use_broker_chain is true")
			}
		} else if strings.TrimSpace(raw.Chain.RPCURL) == "" {
			return errors.New("chain.rpc_url is required when executing on-chain transactions")
		}
	}
	if strings.TrimSpace(raw.MySQL.DSN) != "" {
		parsed, err := mysql.ParseDSN(raw.MySQL.DSN)
		if err != nil || strings.TrimSpace(parsed.DBName) == "" {
			return errors.New("mysql.dsn is invalid")
		}
	}
	if raw.Runtime.Mode == ModeExecute && strings.TrimSpace(raw.MySQL.DSN) == "" {
		return errors.New("mysql.dsn is required when runtime.mode is execute")
	}
	return nil
}

func validateBKCAmountRange(name string, minRaw string, maxRaw string) error {
	minWei, err := amount.ParseBKCToWei(minRaw)
	if err != nil {
		return fmt.Errorf("%s_min_bkc is invalid: %w", name, err)
	}
	maxWei, err := amount.ParseBKCToWei(maxRaw)
	if err != nil {
		return fmt.Errorf("%s_max_bkc is invalid: %w", name, err)
	}
	if minWei.Sign() <= 0 || maxWei.Sign() <= 0 {
		return fmt.Errorf("%s_min_bkc and %s_max_bkc must be positive", name, name)
	}
	if minWei.Cmp(maxWei) > 0 {
		return fmt.Errorf("%s_min_bkc must be <= %s_max_bkc", name, name)
	}
	return nil
}

func buildConfig(raw *rawConfig) *Config {
	return &Config{
		Runtime: RuntimeConfig{
			Enabled:        raw.Runtime.Enabled,
			Mode:           raw.Runtime.Mode,
			OnChain:        raw.Runtime.OnChain,
			DryRun:         raw.Runtime.DryRun || raw.Runtime.Mode == ModePreview,
			PlanFile:       strings.TrimSpace(raw.Runtime.PlanFile),
			ApproveOnChain: raw.Runtime.ApproveOnChain,
		},
		Chain: ChainConfig{
			PrivateKey:      strings.TrimSpace(raw.Chain.PrivateKey),
			ContractAddress: common.HexToAddress(raw.Chain.ContractAddress).Hex(),
			RPCURL:          strings.TrimSpace(raw.Chain.RPCURL),
			BrokerChainURL:  strings.TrimSpace(raw.Chain.BrokerChainURL),
			UseBrokerChain:  raw.Chain.UseBrokerChain,
		},
		MySQL: MySQLConfig{
			DSN:                   strings.TrimSpace(raw.MySQL.DSN),
			MaxOpenConnections:    raw.MySQL.MaxOpenConnections,
			MaxIdleConnections:    raw.MySQL.MaxIdleConnections,
			ConnectionMaxLifetime: time.Duration(raw.MySQL.ConnectionMaxLifetimeSeconds) * time.Second,
		},
		Scenario: ScenarioConfig{
			Type:               raw.Scenario.Type,
			MarketCount:        raw.Scenario.MarketCount,
			ExistingGameIDs:    append([]int(nil), raw.Scenario.ExistingGameIDs...),
			Participants:       raw.Scenario.Participants,
			TradesPerMarketMin: raw.Scenario.TradesPerMarketMin,
			TradesPerMarketMax: raw.Scenario.TradesPerMarketMax,
		},
		Market: MarketConfig{
			Types:                  append([]string(nil), raw.Market.Types...),
			InitialLiquidityMinBKC: raw.Market.InitialLiquidityMinBKC,
			InitialLiquidityMaxBKC: raw.Market.InitialLiquidityMaxBKC,
			DurationMin:            time.Duration(raw.Market.DurationMinSeconds) * time.Second,
			DurationMax:            time.Duration(raw.Market.DurationMaxSeconds) * time.Second,
		},
		Trade: TradeConfig{
			BuyMinBKC:         raw.Trade.BuyMinBKC,
			BuyMaxBKC:         raw.Trade.BuyMaxBKC,
			CreatorAlsoTrades: raw.Trade.CreatorAlsoTrades,
		},
		Timing: TimingConfig{
			Pause:   time.Duration(raw.Timing.PauseSeconds * float64(time.Second)),
			Timeout: time.Duration(raw.Timing.TimeoutSeconds) * time.Second,
		},
	}
}
