package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadReadsSimulatorConfig(t *testing.T) {
	path := writeConfig(t, `runtime:
  enabled: true
  mode: "preview"
  on_chain: false
  dry_run: true
  plan_file: "out/custom-plan.json"
  approve_on_chain: false
chain:
  private_key: "replace-with-private-key"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: "http://127.0.0.1:42515"
  broker_chain_url: "http://127.0.0.1:56741/"
  use_broker_chain: false
mysql:
  dsn: "root:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
scenario:
  type: "create_and_trade"
  market_count: 2
  existing_game_ids: [7]
  participants: 5
  trades_per_market_min: 3
  trades_per_market_max: 6
market:
  types: ["TYPE_PRICE", "TYPE_EVENT"]
  initial_liquidity_min_bkc: "3"
  initial_liquidity_max_bkc: "9"
  duration_min_seconds: 3600
  duration_max_seconds: 7200
trade:
  buy_min_bkc: "0.2"
  buy_max_bkc: "2"
  creator_also_trades: true
timing:
  pause_seconds: 0.5
  timeout_seconds: 90
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Runtime.Enabled || cfg.Runtime.OnChain || !cfg.Runtime.DryRun {
		t.Fatalf("unexpected runtime config: %+v", cfg.Runtime)
	}
	if cfg.Runtime.Mode != ModePreview || cfg.Runtime.PlanFile != "out/custom-plan.json" || cfg.Runtime.ApproveOnChain {
		t.Fatalf("unexpected preview runtime config: %+v", cfg.Runtime)
	}
	if cfg.Scenario.Type != ScenarioCreateAndTrade || cfg.Scenario.MarketCount != 2 || cfg.Scenario.Participants != 5 {
		t.Fatalf("unexpected scenario config: %+v", cfg.Scenario)
	}
	if cfg.Timing.Pause != 500*time.Millisecond || cfg.Timing.Timeout != 90*time.Second {
		t.Fatalf("unexpected timing config: %+v", cfg.Timing)
	}
	if len(cfg.Market.Types) != 2 || cfg.Market.Types[1] != "TYPE_EVENT" {
		t.Fatalf("unexpected market types: %#v", cfg.Market.Types)
	}
}

func TestLoadAppliesReasonableDefaults(t *testing.T) {
	path := writeConfig(t, `runtime:
  enabled: true
chain:
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: "http://127.0.0.1:42515"
mysql:
  dsn: "root:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scenario.Type != ScenarioCreateAndTrade {
		t.Fatalf("scenario type = %q, want %q", cfg.Scenario.Type, ScenarioCreateAndTrade)
	}
	if cfg.Scenario.MarketCount != 1 || cfg.Scenario.Participants != 6 {
		t.Fatalf("unexpected default scenario: %+v", cfg.Scenario)
	}
	if cfg.Market.InitialLiquidityMinBKC != "3" || cfg.Market.InitialLiquidityMaxBKC != "12" {
		t.Fatalf("unexpected default market amounts: %+v", cfg.Market)
	}
	if len(cfg.Market.Types) != 8 {
		t.Fatalf("default market types = %d, want 8", len(cfg.Market.Types))
	}
	if cfg.Runtime.Mode != ModeExecute {
		t.Fatalf("default runtime mode = %q, want %q", cfg.Runtime.Mode, ModeExecute)
	}
	if cfg.Runtime.PlanFile == "" {
		t.Fatal("default runtime plan file is empty")
	}
}

func TestLoadPreviewModeDoesNotRequireExecutionCredentials(t *testing.T) {
	path := writeConfig(t, `runtime:
  enabled: true
  mode: "preview"
  on_chain: true
  plan_file: "out/preview.json"
chain:
  private_key: "replace-with-funded-private-key"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: ""
mysql:
  dsn: ""
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime.Mode != ModePreview || !cfg.Runtime.OnChain {
		t.Fatalf("unexpected runtime config: %+v", cfg.Runtime)
	}
}

func TestLoadRejectsExecuteOnChainWithoutApproval(t *testing.T) {
	path := writeConfig(t, `runtime:
  enabled: true
  mode: "execute"
  on_chain: true
  plan_file: "out/preview.json"
  approve_on_chain: false
chain:
  private_key: "abc123"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: "http://127.0.0.1:8545"
mysql:
  dsn: "root:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded, want on-chain approval error")
	}
}

func TestLoadRejectsUnknownMarketType(t *testing.T) {
	path := writeConfig(t, `runtime:
  enabled: true
chain:
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: "http://127.0.0.1:42515"
mysql:
  dsn: "root:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
market:
  types: ["TYPE_NOT_REAL"]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded, want unknown type error")
	}
}

func TestLoadRejectsOffchainExistingTradesOutsideDryRun(t *testing.T) {
	path := writeConfig(t, `runtime:
  enabled: true
  on_chain: false
  dry_run: false
chain:
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
mysql:
  dsn: "root:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
scenario:
  type: "trade_existing"
  existing_game_ids: [1]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded, want offchain trade_existing error")
	}
}

func TestLoadRejectsReversedAmountRange(t *testing.T) {
	path := writeConfig(t, `runtime:
  enabled: true
chain:
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
mysql:
  dsn: "root:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
trade:
  buy_min_bkc: "3"
  buy_max_bkc: "1"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded, want reversed amount range error")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
