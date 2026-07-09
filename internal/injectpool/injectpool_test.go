package injectpool

import (
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/chain"
)

func TestParseBKCToWei(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "whole", raw: "1", want: "1000000000000000000"},
		{name: "fraction", raw: "1.25", want: "1250000000000000000"},
		{name: "leading decimal point", raw: ".5", want: "500000000000000000"},
		{name: "smallest unit", raw: "0.000000000000000001", want: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBKCToWei(tt.raw)
			if err != nil {
				t.Fatalf("parseBKCToWei(%q) error: %v", tt.raw, err)
			}
			if got.String() != tt.want {
				t.Fatalf("parseBKCToWei(%q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseBKCToWeiRejectsInvalidAmounts(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "1.0000000000000000001", "abc"} {
		if _, err := parseBKCToWei(raw); err == nil {
			t.Fatalf("parseBKCToWei(%q) succeeded, want error", raw)
		}
	}
}

func TestParseOptionPattern(t *testing.T) {
	got, err := parseOptionPattern("yes,no,0,1,Y,N")
	if err != nil {
		t.Fatalf("parseOptionPattern error: %v", err)
	}
	want := []int{0, 1, 0, 1, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pattern[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestLoadParticipantKeysAllowsAutomaticRandomMode(t *testing.T) {
	keys, err := loadParticipantKeys("", "", nil)
	if err != nil {
		t.Fatalf("loadParticipantKeys returned error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("loadParticipantKeys returned %d keys, want 0", len(keys))
	}
}

func TestLoadParticipantKeysIncludesConfigKeys(t *testing.T) {
	keys, err := loadParticipantKeys("", "", []string{" 0xabc ", "", "def"})
	if err != nil {
		t.Fatalf("loadParticipantKeys returned error: %v", err)
	}
	if strings.Join(keys, ",") != "0xabc,def" {
		t.Fatalf("keys = %#v, want trimmed config keys", keys)
	}
}

func TestApplyTestInjectionConfigUsesYAMLDefaults(t *testing.T) {
	cfg := &rawConfig{}
	cfg.TestInjection.Enabled = true
	cfg.TestInjection.GameID = 42
	cfg.TestInjection.Participants = 9
	cfg.TestInjection.AmountBKC = "1.5"
	cfg.TestInjection.RandomMinBKC = "0.3"
	cfg.TestInjection.RandomMaxBKC = "3"
	cfg.TestInjection.Options = "yes,no,no"
	cfg.TestInjection.Keys = []string{"config-key"}
	cfg.TestInjection.KeysFile = "config-keys.txt"
	cfg.TestInjection.PauseSeconds = 1.25
	cfg.TestInjection.TimeoutSeconds = 95

	opts := defaultOptions()
	applyTestInjectionConfig(opts, cfg, map[string]bool{})

	if !opts.enabled || opts.gameID != 42 || opts.participants != 9 {
		t.Fatalf("basic config defaults were not applied: %+v", opts)
	}
	if opts.amountBKC != "1.5" || opts.randomMinBKC != "0.3" || opts.randomMaxBKC != "3" || opts.optionPattern != "yes,no,no" {
		t.Fatalf("amount/option defaults were not applied: %+v", opts)
	}
	if opts.keysFile != "config-keys.txt" || len(opts.configKeys) != 1 || opts.configKeys[0] != "config-key" {
		t.Fatalf("key defaults were not applied: %+v", opts)
	}
	if opts.pause != 1250*time.Millisecond || opts.timeout != 95*time.Second {
		t.Fatalf("time defaults were not applied: pause=%s timeout=%s", opts.pause, opts.timeout)
	}
}

func TestApplyTestInjectionConfigLetsCLIOverridesWin(t *testing.T) {
	cfg := &rawConfig{}
	cfg.TestInjection.Enabled = true
	cfg.TestInjection.GameID = 42
	cfg.TestInjection.Participants = 9
	cfg.TestInjection.AmountBKC = "1.5"
	cfg.TestInjection.RandomMinBKC = "0.3"
	cfg.TestInjection.RandomMaxBKC = "3"
	cfg.TestInjection.Options = "yes,no,no"
	cfg.TestInjection.Keys = []string{"config-key"}
	cfg.TestInjection.KeysFile = "config-keys.txt"
	cfg.TestInjection.PauseSeconds = 1.25
	cfg.TestInjection.TimeoutSeconds = 95

	opts := defaultOptions()
	opts.enabled = false
	opts.gameID = 7
	opts.participants = 2
	opts.amountBKC = "9"
	opts.randomMinBKC = "8"
	opts.randomMaxBKC = "10"
	opts.optionPattern = "no"
	opts.keysRaw = "cli-key"
	opts.keysFile = "cli-keys.txt"
	opts.pause = 3 * time.Second
	opts.timeout = 4 * time.Second

	applyTestInjectionConfig(opts, cfg, map[string]bool{
		"enabled":        true,
		"game-id":        true,
		"participants":   true,
		"amount-bkc":     true,
		"random-min-bkc": true,
		"random-max-bkc": true,
		"options":        true,
		"keys":           true,
		"keys-file":      true,
		"pause":          true,
		"timeout":        true,
	})

	if opts.enabled || opts.gameID != 7 || opts.participants != 2 {
		t.Fatalf("CLI basics were overwritten by config: %+v", opts)
	}
	if opts.amountBKC != "9" || opts.randomMinBKC != "8" || opts.randomMaxBKC != "10" || opts.optionPattern != "no" {
		t.Fatalf("CLI amount/options were overwritten by config: %+v", opts)
	}
	if opts.keysRaw != "cli-key" || opts.keysFile != "cli-keys.txt" || len(opts.configKeys) != 0 {
		t.Fatalf("CLI keys were overwritten by config: %+v", opts)
	}
	if opts.pause != 3*time.Second || opts.timeout != 4*time.Second {
		t.Fatalf("CLI timeouts were overwritten by config: pause=%s timeout=%s", opts.pause, opts.timeout)
	}
}

func TestRunCLIRequiresEnabledForRealInjection(t *testing.T) {
	path := writeInjectpoolConfig(t, `
  enabled: false
  game_id: 1
  participants: 1
  random_min_bkc: "0.2"
  random_max_bkc: "0.2"
`)
	err := RunCLI([]string{"-config", path})
	if err == nil || !strings.Contains(err.Error(), "test_injection.enabled") {
		t.Fatalf("RunCLI error = %v, want disabled injection error", err)
	}
}

func TestRunCLIAllowsDryRunWhenDisabled(t *testing.T) {
	path := writeInjectpoolConfig(t, `
  enabled: false
  game_id: 1
  participants: 2
  random_min_bkc: "0.2"
  random_max_bkc: "0.2"
`)
	if err := RunCLI([]string{"-config", path, "-dry-run"}); err != nil {
		t.Fatalf("RunCLI dry-run error: %v", err)
	}
}

func TestBuildRandomParticipantsUsesReasonableRanges(t *testing.T) {
	minAmount, err := parseBKCToWei("0.2")
	if err != nil {
		t.Fatal(err)
	}
	maxAmount, err := parseBKCToWei("2")
	if err != nil {
		t.Fatal(err)
	}
	stepAmount, err := parseBKCToWei("0.01")
	if err != nil {
		t.Fatal(err)
	}

	participants, err := buildRandomParticipants(7, nil, minAmount, maxAmount)
	if err != nil {
		t.Fatalf("buildRandomParticipants error: %v", err)
	}
	if len(participants) != 7 {
		t.Fatalf("participant count = %d, want 7", len(participants))
	}

	seenAddresses := map[string]struct{}{}
	yesCount := 0
	noCount := 0
	for _, p := range participants {
		if p.privateKey == "" {
			t.Fatal("generated participant has empty private key")
		}
		if p.address == "" {
			t.Fatal("generated participant has empty address")
		}
		if _, ok := seenAddresses[p.address]; ok {
			t.Fatalf("duplicate generated address %s", p.address)
		}
		seenAddresses[p.address] = struct{}{}
		if p.amountWei.Cmp(minAmount) < 0 || p.amountWei.Cmp(maxAmount) > 0 {
			t.Fatalf("amount %s outside range [%s,%s]", p.amountWei, minAmount, maxAmount)
		}
		if new(big.Int).Mod(p.amountWei, stepAmount).Sign() != 0 {
			t.Fatalf("amount %s is not aligned to 0.01 BKC", p.amountWei)
		}
		switch p.optionID {
		case 0:
			yesCount++
		case 1:
			noCount++
		default:
			t.Fatalf("optionID = %d, want 0 or 1", p.optionID)
		}
	}
	if yesCount < 3 || noCount < 3 {
		t.Fatalf("options are not balanced enough: yes=%d no=%d", yesCount, noCount)
	}
}

func TestGeneratedParticipantFundingCoversStakeAndBuyGas(t *testing.T) {
	stake := big.NewInt(1_000_000_000_000_000_000)
	gasPrice := big.NewInt(2)
	got := generatedParticipantFunding(stake, gasPrice)

	want := new(big.Int).Set(stake)
	want.Add(want, new(big.Int).Mul(gasPrice, big.NewInt(generatedBuyGasLimit)))
	want.Add(want, generatedFundingBufferWei())
	if got.Cmp(want) != 0 {
		t.Fatalf("generatedParticipantFunding = %s, want %s", got, want)
	}
}

func TestShareDeltaForOption(t *testing.T) {
	before := &chain.GameExtraData{MySharesYESNO: []*big.Int{big.NewInt(10), big.NewInt(4)}}
	after := &chain.GameExtraData{MySharesYESNO: []*big.Int{big.NewInt(17), big.NewInt(4)}}

	got := shareDeltaForOption(before, after, 0)
	if got.String() != "7" {
		t.Fatalf("shareDeltaForOption YES = %s, want 7", got)
	}

	got = shareDeltaForOption(after, before, 0)
	if got.Sign() != 0 {
		t.Fatalf("negative share delta should clamp to zero, got %s", got)
	}
}

func TestPricesFromExtraUsesOppositeReserveForYes(t *testing.T) {
	extra := &chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{big.NewInt(75), big.NewInt(25)},
	}
	yes, no, err := pricesFromExtra(extra)
	if err != nil {
		t.Fatalf("pricesFromExtra error: %v", err)
	}
	if math.Abs(yes-75) > 0.000001 {
		t.Fatalf("yes price = %f, want 75", yes)
	}
	if math.Abs(no-25) > 0.000001 {
		t.Fatalf("no price = %f, want 25", no)
	}
}

func writeInjectpoolConfig(t *testing.T, injectionYAML string) string {
	t.Helper()
	body := `chain:
  private_key: "0000000000000000000000000000000000000000000000000000000000000001"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: "http://127.0.0.1:42515"
  broker_chain_url: "http://127.0.0.1:56741/"
  use_broker_chain: false
mysql:
  dsn: "root:secret@tcp(127.0.0.1:3306)/predictionmarket_local?parseTime=true"
  max_open_connections: 10
  max_idle_connections: 2
  connection_max_lifetime_seconds: 300
test_injection:` + injectionYAML
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
