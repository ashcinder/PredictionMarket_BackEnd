package aicenter

import (
	"math"
	"testing"
)

func TestProbabilitiesFromReason(t *testing.T) {
	estimated, market := probabilitiesFromReason(
		"trend supports YES | est_prob=0.73 market_prob=0.61",
	)
	if math.Abs(estimated-.73) > .0001 || math.Abs(market-.61) > .0001 {
		t.Fatalf("unexpected probabilities: estimated=%f market=%f", estimated, market)
	}
}

func TestBuildOpportunitiesKeepsOnlyLatestQualifiedDecisionPerMarket(t *testing.T) {
	decisions := []ManagedDecision{
		{
			ID: 2, ContractAddress: "0xabc", GameID: 1, Action: "buy_yes",
			Confidence: .82, EstimatedProbYES: .72, MarketProbYES: .61,
			ProbabilityEdgePct: 11, Outcome: "traded",
		},
		{
			ID: 1, ContractAddress: "0xabc", GameID: 1, Action: "buy_no",
			Confidence: .90, EstimatedProbYES: .20, MarketProbYES: .60,
			ProbabilityEdgePct: -40, Outcome: "traded",
		},
		{
			ID: 3, ContractAddress: "0xdef", GameID: 2, Action: "buy_yes",
			Confidence: .60, EstimatedProbYES: .80, MarketProbYES: .60,
			ProbabilityEdgePct: 20, Outcome: "low_confidence",
		},
	}
	opportunities := buildOpportunities(decisions)
	if len(opportunities) != 1 {
		t.Fatalf("expected one qualified opportunity, got %+v", opportunities)
	}
	if opportunities[0].Side != "YES" || opportunities[0].DecisionID != 2 {
		t.Fatalf("latest qualified direction was not preserved: %+v", opportunities[0])
	}
}

func TestBuildOpportunitiesNormalizesNOEdge(t *testing.T) {
	opportunities := buildOpportunities([]ManagedDecision{{
		ID: 4, ContractAddress: "0xabc", GameID: 4, Action: "buy_no",
		Confidence: .88, EstimatedProbYES: .28, MarketProbYES: .43,
		ProbabilityEdgePct: -15, Outcome: "traded",
	}})
	if len(opportunities) != 1 || opportunities[0].Side != "NO" {
		t.Fatalf("expected NO opportunity, got %+v", opportunities)
	}
	if math.Abs(opportunities[0].EstimatedProb-.72) > .0001 ||
		math.Abs(opportunities[0].MarketProb-.57) > .0001 ||
		math.Abs(opportunities[0].EdgePercent-15) > .0001 {
		t.Fatalf("NO probabilities were not normalized: %+v", opportunities[0])
	}
}
