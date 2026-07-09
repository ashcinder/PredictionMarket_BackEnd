package sim

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"predictionmarket-simulator/internal/config"
	dbwriter "predictionmarket-simulator/internal/db"
)

func TestExecuteOffchainTradeMatchesContractBuyYesFormula(t *testing.T) {
	initial := big.NewInt(100)
	state := newOffchainMarketState(7, "sim-cid", initial, 12345)

	trade, err := executeOffchainTrade(state, "0x1234567890123456789012345678901234567890", 0, big.NewInt(25))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := state.reserveNo.String(), "125"; got != want {
		t.Fatalf("reserveNo = %s, want %s", got, want)
	}
	if got, want := state.reserveYes.String(), "80"; got != want {
		t.Fatalf("reserveYes = %s, want %s", got, want)
	}
	if got, want := trade.ShareAmountWei.String(), "45"; got != want {
		t.Fatalf("share amount = %s, want %s", got, want)
	}
	if got, want := dbwriter.ShareAt(trade.Extra.VirtualReservesNOYES, 0).String(), "125"; got != want {
		t.Fatalf("extra reserve NO = %s, want %s", got, want)
	}
	if got, want := dbwriter.ShareAt(trade.Extra.VirtualReservesNOYES, 1).String(), "80"; got != want {
		t.Fatalf("extra reserve YES = %s, want %s", got, want)
	}
	if got, want := trade.MySharesYesAfter, "45"; got != want {
		t.Fatalf("YES shares after = %s, want %s", got, want)
	}
}

func TestRunPreviewWritesReviewablePlanFile(t *testing.T) {
	planFile := filepath.Join(t.TempDir(), "simulator-plan.json")
	cfg := &config.Config{
		Runtime: config.RuntimeConfig{
			Enabled:  true,
			Mode:     config.ModePreview,
			OnChain:  true,
			PlanFile: planFile,
		},
		Chain: config.ChainConfig{
			ContractAddress: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		},
		Scenario: config.ScenarioConfig{
			Type:               config.ScenarioCreateAndTrade,
			MarketCount:        2,
			Participants:       3,
			TradesPerMarketMin: 1,
			TradesPerMarketMax: 1,
		},
		Market: config.MarketConfig{
			Types:                  []string{"TYPE_PRICE", "TYPE_EVENT"},
			InitialLiquidityMinBKC: "3",
			InitialLiquidityMaxBKC: "3",
			DurationMin:            time.Hour,
			DurationMax:            time.Hour,
		},
		Trade: config.TradeConfig{
			BuyMinBKC: "0.2",
			BuyMaxBKC: "0.2",
		},
		Timing: config.TimingConfig{
			Timeout: 5 * time.Second,
		},
	}

	if err := New(cfg, nil).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	plan, err := readPlan(planFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Participants) != 3 {
		t.Fatalf("participants = %d, want 3", len(plan.Participants))
	}
	if len(plan.Markets) != 2 {
		t.Fatalf("markets = %d, want 2", len(plan.Markets))
	}
	if got := plan.Markets[0].Type; got != "TYPE_PRICE" {
		t.Fatalf("first market type = %q, want TYPE_PRICE", got)
	}
	if len(plan.Markets[0].Trades) != 1 {
		t.Fatalf("first market trades = %d, want 1", len(plan.Markets[0].Trades))
	}
	if plan.Participants[0].PrivateKey == "" || plan.Participants[0].Address == "" {
		t.Fatalf("preview plan did not include replayable participant credentials: %+v", plan.Participants[0])
	}
}

func TestExecutePlannedTradesRejectsUserAddressMismatch(t *testing.T) {
	cfg := &config.Config{
		Runtime: config.RuntimeConfig{Mode: config.ModeExecute},
		Timing:  config.TimingConfig{Timeout: time.Second},
	}
	participants := []participant{{
		privateKey: "abc",
		address:    "0x1234567890123456789012345678901234567890",
	}}
	trades := []PlanTrade{{
		Index:     1,
		UserIndex: 0,
		User:      "0x9999999999999999999999999999999999999999",
		OptionID:  0,
		AmountWei: "1",
	}}

	err := New(cfg, nil).executePlannedTradesForMarket(context.Background(), nil, participants, 1, nil, trades)
	if err == nil {
		t.Fatal("executePlannedTradesForMarket succeeded, want user mismatch error")
	}
}
