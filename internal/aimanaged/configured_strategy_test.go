package aimanaged

import (
	"testing"
)

func TestGridStrategyBuildsBuyAndSellSignalsFromConfiguredBand(t *testing.T) {
	strategy := StrategySettings{
		StrategyType: "grid", Direction: "yes", BuyAmountBKC: "1",
		ConfidenceMin: .70, MinEdgePercent: 5, KellyFraction: .25,
		AdaptiveCooldown: true, GridLowerPercent: 30,
		GridUpperPercent: 70, GridLevels: 8,
	}
	snapshot := EntrySnapshot{GameID: 1, Strategy: &strategy}

	_, buy := buildConfiguredStrategyDecision(snapshot, strategy, .35)
	if buy.Action != "buy_yes" || buy.ExitAction != "hold" {
		t.Fatalf("expected low-band YES buy, got %+v", buy)
	}
	_, sell := buildConfiguredStrategyDecision(snapshot, strategy, .65)
	if sell.Action != "hold" || sell.ExitAction != "sell_yes" {
		t.Fatalf("expected high-band YES sell, got %+v", sell)
	}
	_, hold := buildConfiguredStrategyDecision(snapshot, strategy, .50)
	if hold.Action != "hold" || hold.ExitAction != "hold" {
		t.Fatalf("expected neutral-band hold, got %+v", hold)
	}
}

func TestMartingaleStrategyScalesConfiguredOrderAtDeeperProbabilityLevels(t *testing.T) {
	strategy := StrategySettings{
		StrategyType: "martingale", Direction: "no", BuyAmountBKC: "1",
		ConfidenceMin: .70, MinEdgePercent: 5, KellyFraction: .25,
		AdaptiveCooldown: true, MartingaleTriggerPercent: 45,
		MartingaleMultiplier: 2, MartingaleMaxRounds: 4,
	}
	snapshot := EntrySnapshot{GameID: 1, Strategy: &strategy}

	scaled, decision := buildConfiguredStrategyDecision(snapshot, strategy, .90)
	if decision.Action != "buy_no" {
		t.Fatalf("expected NO martingale buy, got %+v", decision)
	}
	if scaled.Strategy == nil || scaled.Strategy.BuyAmountBKC != "8.000000" {
		t.Fatalf("expected fourth-level 8 BKC order, got %+v", scaled.Strategy)
	}
}

func TestCustomStrategyValidationRejectsInvalidConfiguration(t *testing.T) {
	_, err := validateStrategy(&StrategySettings{
		StrategyType: "grid", Direction: "yes", BuyAmountBKC: "1",
		ConfidenceMin: .70, MinEdgePercent: 5, KellyFraction: .25,
		GridLowerPercent: 70, GridUpperPercent: 30, GridLevels: 1,
	})
	if err == nil {
		t.Fatal("expected invalid grid configuration to be rejected")
	}
}
