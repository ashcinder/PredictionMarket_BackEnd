package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validYAML = `chain:
  private_key: "0000000000000000000000000000000000000000000000000000000000000001"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: ""
  broker_chain_url: "https://dash.broker-chain.com:443/"
  use_broker_chain: true
server:
  http_listen: ":8081"
mysql:
  dsn: "prediction:secret@tcp(127.0.0.1:3306)/prediction_market?charset=utf8mb4&parseTime=true&loc=UTC"
  max_open_connections: 10
  max_idle_connections: 5
  connection_max_lifetime_seconds: 300
ipfs:
  gateway: "http://127.0.0.1:8080/ipfs"
  fallback_gateways:
    - "http://127.0.0.1:8083/ipfs"
oracle:
  gold_api_url: "https://api.gold-api.com/price/XAU"
  chainlink_rpc_urls:
    - "https://ethereum-rpc.publicnode.com"
    - "https://eth.llamarpc.com"
  chainlink_xau_usd_feed: "0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6"
  chainlink_btc_usd_feed: "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c"
  chainlink_poll_interval_seconds: 60
  chainlink_max_staleness_seconds: 43200
  sina_url: "https://hq.sinajs.cn/list=hf_XAU"
  sina_referer: "https://finance.sina.com.cn"
  user_agent: "PredictionMarket/1.0"
  request_timeout_seconds: 10
sentinel:
  auto_resolve_enabled: true
  poll_interval_seconds: 30
  resolve_delay_seconds: 5
sampler:
  chain_sync_enabled: false
  poll_interval_seconds: 60
ai:
  api_key: "test-ai-key"
  base_url: "https://api.deepseek.com/chat/completions"
  model: "deepseek-chat"
  poll_interval_seconds: 120
  buy_amount_bkc: "10"
  confidence_min: 0.70
  history_min_points: 3
  history_max_points: 256
`

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFileReadsCompleteYAML(t *testing.T) {
	cfg, err := LoadFile(writeTestConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIAPIKey != "test-ai-key" || cfg.AIModel != "deepseek-chat" {
		t.Fatalf("unexpected AI config: %+v", cfg)
	}
	if cfg.IPFSGateway != "http://127.0.0.1:8080/ipfs/" {
		t.Fatalf("IPFS gateway was not normalized: %q", cfg.IPFSGateway)
	}
	if len(cfg.IPFSFallbackGateways) != 1 || cfg.IPFSFallbackGateways[0] != "http://127.0.0.1:8083/ipfs/" {
		t.Fatalf("IPFS fallback gateways were not normalized: %#v", cfg.IPFSFallbackGateways)
	}
	if cfg.GoldAPIURL != "https://api.gold-api.com/price/XAU" ||
		cfg.SinaURL != "https://hq.sinajs.cn/list=hf_XAU" ||
		cfg.OracleRequestTimeout != 10*time.Second || cfg.OracleSampleInterval != 10*time.Second {
		t.Fatalf("unexpected oracle config: %+v", cfg)
	}
	if len(cfg.ChainlinkRPCURLs) != 2 ||
		cfg.ChainlinkXAUUSDFeed != "0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6" ||
		cfg.ChainlinkBTCUSDFeed != "0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c" ||
		cfg.ChainlinkPollInterval != time.Minute || cfg.ChainlinkMaxStaleness != 12*time.Hour {
		t.Fatalf("unexpected Chainlink config: %+v", cfg)
	}
	if cfg.PollInterval != 30*time.Second || cfg.AIPollInterval != 120*time.Second || cfg.SamplerPollInterval != 60*time.Second {
		t.Fatalf("unexpected intervals: poll=%s ai=%s sampler=%s", cfg.PollInterval, cfg.AIPollInterval, cfg.SamplerPollInterval)
	}
	if !cfg.AutoResolveEnabled {
		t.Fatal("sentinel.auto_resolve_enabled = false, want true")
	}
	if cfg.SamplerChainSyncEnabled {
		t.Fatal("sampler.chain_sync_enabled = true, want false")
	}
	if cfg.AIHistoryMinPoints != 3 || cfg.AIHistoryMaxPoints != 256 {
		t.Fatalf("unexpected AI history settings: min=%d max=%d", cfg.AIHistoryMinPoints, cfg.AIHistoryMaxPoints)
	}
	if cfg.MySQLDSN == "" || cfg.MySQLMaxOpenConnections != 10 ||
		cfg.MySQLMaxIdleConnections != 5 || cfg.MySQLConnectionMaxLifetime != 300*time.Second {
		t.Fatalf("unexpected MySQL config: %+v", cfg)
	}
}

func TestLoadFileDefaultsAutomaticResolutionToEnabled(t *testing.T) {
	body := strings.Replace(validYAML, "  auto_resolve_enabled: true\n", "", 1)
	cfg, err := LoadFile(writeTestConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoResolveEnabled {
		t.Fatal("omitted sentinel.auto_resolve_enabled must preserve automatic resolution")
	}
}

func TestLoadFileCanDisableAutomaticResolution(t *testing.T) {
	body := strings.Replace(validYAML,
		"auto_resolve_enabled: true", "auto_resolve_enabled: false", 1)
	cfg, err := LoadFile(writeTestConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoResolveEnabled {
		t.Fatal("sentinel.auto_resolve_enabled = true, want false")
	}
}

func TestLoadFileEnablesSamplerChainSync(t *testing.T) {
	body := strings.Replace(validYAML, "chain_sync_enabled: false", "chain_sync_enabled: true", 1)
	cfg, err := LoadFile(writeTestConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SamplerChainSyncEnabled {
		t.Fatal("sampler.chain_sync_enabled = false, want true")
	}
}

func TestRuntimeMySQLDSNOverride(t *testing.T) {
	cfg, err := LoadFile(writeTestConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	const override = "local:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
	t.Setenv(MySQLDSNEnvName, override)
	if err := applyRuntimeOverrides(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MySQLDSN != override {
		t.Fatalf("MySQL DSN=%q, want environment override", cfg.MySQLDSN)
	}
}

func TestRuntimeMySQLDatabaseOverride(t *testing.T) {
	cfg, err := LoadFile(writeTestConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(MySQLDatabaseEnvName, "predictionmarket_local")
	if err := applyRuntimeOverrides(cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.MySQLDSN, "/predictionmarket_local?") {
		t.Fatalf("MySQL DSN did not use local database: %q", cfg.MySQLDSN)
	}
}

func TestCNSupervisorBranchProfile(t *testing.T) {
	cfg, err := LoadFile(writeTestConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := applyBranchProfile(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.UseBrokerChain {
		t.Fatal("local branch unexpectedly enabled BrokerChain")
	}
	if cfg.RPCURL != "http://127.0.0.1:42515" {
		t.Fatalf("RPC URL=%q", cfg.RPCURL)
	}
	if !strings.Contains(cfg.MySQLDSN, "/predictionmarket_cn?") {
		t.Fatalf("CN branch database was not selected: %q", cfg.MySQLDSN)
	}
}

func TestLoadFileRejectsInvalidConfiguration(t *testing.T) {
	tests := map[string]string{
		"wallet key": strings.Replace(validYAML,
			"0000000000000000000000000000000000000000000000000000000000000001",
			"replace-with-wallet-private-key", 1),
		"AI key": strings.Replace(validYAML, "test-ai-key", "replace-with-ai-api-key", 1),
		"contract": strings.Replace(validYAML,
			"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c", "not-an-address", 1),
		"RPC mode without URL": strings.Replace(validYAML,
			"use_broker_chain: true", "use_broker_chain: false", 1),
		"AI URL": strings.Replace(validYAML,
			"https://api.deepseek.com/chat/completions", "ftp://invalid", 1),
		"oracle URL": strings.Replace(validYAML,
			"https://api.gold-api.com/price/XAU", "ftp://invalid", 1),
		"Chainlink RPC URL": strings.Replace(validYAML,
			"https://ethereum-rpc.publicnode.com", "ftp://invalid", 1),
		"Chainlink XAU feed": strings.Replace(validYAML,
			"0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6", "not-an-address", 1),
		"Chainlink poll interval": strings.Replace(validYAML,
			"chainlink_poll_interval_seconds: 60", "chainlink_poll_interval_seconds: 5", 1),
		"Chainlink staleness": strings.Replace(validYAML,
			"chainlink_max_staleness_seconds: 43200", "chainlink_max_staleness_seconds: 10", 1),
		"poll interval": strings.Replace(validYAML,
			"poll_interval_seconds: 30", "poll_interval_seconds: 0", 1),
		"sampler poll interval": strings.Replace(validYAML, "sampler:\n  chain_sync_enabled: false\n  poll_interval_seconds: 60", "sampler:\n  chain_sync_enabled: false\n  poll_interval_seconds: 0", 1),
		"confidence": strings.Replace(validYAML,
			"confidence_min: 0.70", "confidence_min: 1.1", 1),
		"NaN confidence": strings.Replace(validYAML,
			"confidence_min: 0.70", "confidence_min: .nan", 1),
		"buy amount": strings.Replace(validYAML,
			`buy_amount_bkc: "10"`, `buy_amount_bkc: "0"`, 1),
		"NaN buy amount": strings.Replace(validYAML,
			`buy_amount_bkc: "10"`, `buy_amount_bkc: "NaN"`, 1),
		"history minimum": strings.Replace(validYAML,
			"history_min_points: 3", "history_min_points: -1", 1),
		"history maximum": strings.Replace(validYAML,
			"history_max_points: 256", "history_max_points: -1", 1),
		"history range": strings.NewReplacer(
			"history_min_points: 3", "history_min_points: 4",
			"history_max_points: 256", "history_max_points: 3",
		).Replace(validYAML),
		"history maximum API limit": strings.Replace(validYAML,
			"history_max_points: 256", "history_max_points: 1001", 1),
		"MySQL DSN empty": strings.Replace(validYAML,
			`dsn: "prediction:secret@tcp(127.0.0.1:3306)/prediction_market?charset=utf8mb4&parseTime=true&loc=UTC"`, `dsn: ""`, 1),
		"MySQL DSN placeholder": strings.Replace(validYAML,
			`dsn: "prediction:secret@tcp(127.0.0.1:3306)/prediction_market?charset=utf8mb4&parseTime=true&loc=UTC"`, `dsn: "replace-with-mysql-dsn"`, 1),
		"MySQL password placeholder": strings.Replace(validYAML,
			"prediction:secret@tcp", "prediction:replace-with-mysql-password@tcp", 1),
		"MySQL DSN malformed": strings.Replace(validYAML,
			`dsn: "prediction:secret@tcp(127.0.0.1:3306)/prediction_market?charset=utf8mb4&parseTime=true&loc=UTC"`, `dsn: "bad::dsn"`, 1),
		"MySQL DSN without database": strings.Replace(validYAML,
			"/prediction_market?charset", "/?charset", 1),
		"MySQL max open": strings.Replace(validYAML,
			"max_open_connections: 10", "max_open_connections: 0", 1),
		"MySQL max idle": strings.Replace(validYAML,
			"max_idle_connections: 5", "max_idle_connections: 0", 1),
		"MySQL connection lifetime": strings.Replace(validYAML,
			"connection_max_lifetime_seconds: 300", "connection_max_lifetime_seconds: 0", 1),
		"MySQL idle exceeds open": strings.NewReplacer(
			"max_open_connections: 10", "max_open_connections: 4",
			"max_idle_connections: 5", "max_idle_connections: 5",
		).Replace(validYAML),
		"unknown field": validYAML + "unexpected: true\n",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadFile(writeTestConfig(t, body)); err == nil {
				t.Fatal("expected validation error")
			} else if strings.Contains(err.Error(), "prediction:secret") {
				t.Fatalf("validation error leaked MySQL credentials: %v", err)
			}
		})
	}
}

func TestLoadFileReportsMissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileIgnoresAIOraclePlaceholderProviders(t *testing.T) {
	body := validYAML + `
aioracle:
  poll_interval_seconds: 300
  consensus:
    min_consensus_ratio: 0.66
    min_confidence: 0.60
    min_models_required: 1
  news:
    max_articles: 4
    lookback_hours: 24
    request_timeout_seconds: 10
  providers:
    - name: deepseek
      model: deepseek-chat
      api_key: real-test-key
      provider: deepseek
      weight: 1
    - name: openai
      model: gpt-4o
      api_key: sk-...
      provider: openai
      weight: 1
`
	cfg, err := LoadFile(writeTestConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AIOracleProviders) != 1 || cfg.AIOracleProviders[0].Name != "deepseek" {
		t.Fatalf("expected only usable provider, got %+v", cfg.AIOracleProviders)
	}
	if cfg.AIOracleNews.MaxArticles != 4 || cfg.AIOracleNews.LookbackHours != 24 {
		t.Fatalf("unexpected AI oracle news config: %+v", cfg.AIOracleNews)
	}
}

func TestRepositoryConfigurationArtifactsUseYAML(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	example, err := os.ReadFile(filepath.Join(root, "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	usable := strings.ReplaceAll(string(example),
		"replace-with-wallet-private-key",
		"0000000000000000000000000000000000000000000000000000000000000001")
	usable = strings.ReplaceAll(usable, "replace-with-ai-api-key", "test-ai-key")
	usable = strings.ReplaceAll(usable, "replace-with-mysql-password", "test-mysql-password")
	for _, field := range []string{
		"history_min_points", "history_max_points", "dsn", "max_open_connections",
		"max_idle_connections", "connection_max_lifetime_seconds",
	} {
		if !strings.Contains(string(example), field) {
			t.Fatalf("example config does not mention %s", field)
		}
	}
	if _, err := LoadFile(writeTestConfig(t, usable)); err != nil {
		t.Fatalf("example config is not valid after inserting secrets: %v", err)
	}

	for _, name := range []string{"README.md"} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "config.example.xml") ||
			strings.Contains(string(body), "读取 config.xml") {
			t.Fatalf("%s still instructs users to configure XML", name)
		}
		if !strings.Contains(string(body), "config.yaml") {
			t.Fatalf("%s does not mention config.yaml", name)
		}
		if name == "README.md" {
			for _, field := range []string{
				"history_min_points", "history_max_points", "mysql.dsn",
				"docker-compose.mysql.yml", "chainlink_rpc_urls",
				"chainlink_xau_usd_feed", "chainlink_btc_usd_feed",
				"chainlink_eth_usd_feed", "chainlink_sol_usd_feed", "chainlink_bnb_usd_feed",
			} {
				if !strings.Contains(string(body), field) {
					t.Fatalf("%s does not explain %s", name, field)
				}
			}
		}
	}
	compose, err := os.ReadFile(filepath.Join(root, "docker-compose.mysql.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "mysql:8") {
		t.Fatal("docker-compose.mysql.yml does not use MySQL 8")
	}
}
