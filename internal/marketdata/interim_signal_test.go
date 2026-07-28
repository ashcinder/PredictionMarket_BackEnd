package marketdata

import (
	"context"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/chainlinkfeed"
	"PredictionMarket/internal/judge"

	"github.com/ethereum/go-ethereum/common"
)

func TestAnalyzeAtSupportsEveryCreatableVersion2Template(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	end := start.Add(48 * time.Hour)
	now := start.Add(36 * time.Hour)
	repo := newMemoryChainlinkRoundRepo()
	repo.rounds[chainlinkXAUFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkXAUFeed, 1, start.Add(-time.Minute), 4000),
		roundAt(chainlinkXAUFeed, 2, start.Add(24*time.Hour-time.Minute), 4010),
		roundAt(chainlinkXAUFeed, 3, now.Add(-time.Minute), 4040),
		roundAt(chainlinkXAUFeed, 4, end.Add(-time.Minute), 4050),
	}
	repo.rounds[chainlinkBTCFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkBTCFeed, 11, start.Add(-time.Minute), 100000),
		roundAt(chainlinkBTCFeed, 12, now.Add(-time.Minute), 100200),
		roundAt(chainlinkBTCFeed, 13, end.Add(-time.Minute), 100300),
	}
	resolver := NewChainlinkResolver(
		&fakeHistoricalFeedClient{}, repo, chainlinkXAUFeed, chainlinkBTCFeed)

	tests := []struct {
		name       string
		rule       judge.Rule
		wantStatus string
		wantNow    string
		wantText   string
	}{
		{
			name: "price direction",
			rule: func() judge.Rule {
				rule := v2MarketRule(judge.TypePrice, start, end)
				rule.Direction, rule.FlatTolerance = "UP", 0.05
				return rule
			}(),
			wantStatus: "IN_PROGRESS", wantNow: "YES", wantText: "当前收益率",
		},
		{
			name: "absolute return",
			rule: func() judge.Rule {
				rule := v2MarketRule(judge.TypeReturnThreshold, start, end)
				rule.Operator, rule.Threshold = "GTE", 0.5
				return rule
			}(),
			wantStatus: "IN_PROGRESS", wantNow: "YES", wantText: "绝对收益率",
		},
		{
			name: "price threshold",
			rule: func() judge.Rule {
				rule := v2MarketRule(judge.TypePriceThreshold, start, end)
				rule.Operator, rule.Threshold = "GTE", 4030
				return rule
			}(),
			wantStatus: "IN_PROGRESS", wantNow: "YES", wantText: "当前价格",
		},
		{
			name: "price range",
			rule: func() judge.Rule {
				rule := v2MarketRule(judge.TypePriceRange, start, end)
				rule.Operator = "IN_RANGE"
				rule.LowerThreshold, rule.UpperThreshold = 4030, 4050
				return rule
			}(),
			wantStatus: "IN_PROGRESS", wantNow: "YES", wantText: "当前价格",
		},
		{
			name: "relative return with BTC",
			rule: func() judge.Rule {
				rule := v2MarketRule(judge.TypeRelative, start, end)
				rule.Benchmark = "BTC"
				rule.BenchmarkSourceContract = judge.ChainlinkBTCUSDFeed
				return rule
			}(),
			wantStatus: "IN_PROGRESS", wantNow: "YES", wantText: "BTC 收益率",
		},
		{
			name: "consecutive direction",
			rule: func() judge.Rule {
				rule := v2MarketRule(judge.TypeStreak, start, end)
				rule.Direction, rule.StreakDays = "UP", 2
				return rule
			}(),
			wantStatus: "IN_PROGRESS", wantNow: "PENDING", wantText: "1/2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolver.AnalyzeAt(context.Background(), test.rule, now)
			if err != nil {
				t.Fatalf("AnalyzeAt() error = %v", err)
			}
			if got.Status != test.wantStatus || got.CurrentCondition != test.wantNow {
				t.Fatalf("signal status/condition = %s/%s, want %s/%s: %+v",
					got.Status, got.CurrentCondition, test.wantStatus, test.wantNow, got)
			}
			if !strings.Contains(got.Summary, test.wantText) {
				t.Fatalf("summary %q does not contain %q", got.Summary, test.wantText)
			}
			if len(got.Evidence) == 0 {
				t.Fatal("trusted Chainlink evidence is empty")
			}
		})
	}
}

func TestAnalyzeAtRelativeMarketIncludesSynchronizedBenchmarkEvidence(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	end := start.Add(24 * time.Hour)
	now := start.Add(12 * time.Hour)
	repo := newMemoryChainlinkRoundRepo()
	repo.rounds[chainlinkXAUFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkXAUFeed, 1, start.Add(-time.Minute), 4000),
		roundAt(chainlinkXAUFeed, 2, now.Add(-time.Minute), 4040),
	}
	repo.rounds[chainlinkBTCFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkBTCFeed, 11, start.Add(-time.Minute), 100000),
		roundAt(chainlinkBTCFeed, 12, now.Add(-time.Minute), 101500),
	}
	resolver := NewChainlinkResolver(
		&fakeHistoricalFeedClient{}, repo, chainlinkXAUFeed, chainlinkBTCFeed)
	rule := v2MarketRule(judge.TypeRelative, start, end)
	rule.Benchmark, rule.BenchmarkSourceContract = "BTC", judge.ChainlinkBTCUSDFeed

	got, err := resolver.AnalyzeAt(context.Background(), rule, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.BenchmarkSymbol != "BTC" || got.BenchmarkStartPrice != 100000 ||
		got.BenchmarkCurrentPrice != 101500 {
		t.Fatalf("BTC evidence missing from signal: %+v", got)
	}
	if len(got.Evidence) != 4 {
		t.Fatalf("evidence count = %d, want 4 synchronized XAU/BTC rounds", len(got.Evidence))
	}
	if got.PrimaryReturnPct != 1 || got.BenchmarkReturnPct != 1.5 ||
		got.RelativeSpreadPct != -0.5 || got.CurrentCondition != "NO" {
		t.Fatalf("relative calculation is incorrect: %+v", got)
	}
}

func TestAnalyzeAtSupportsEveryConfiguredRelativeBenchmark(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	end := start.Add(24 * time.Hour)
	now := start.Add(12 * time.Hour)
	feeds := map[string]common.Address{
		"BTC": common.HexToAddress(judge.ChainlinkBTCUSDFeed),
		"ETH": common.HexToAddress(judge.ChainlinkETHUSDFeed),
		"SOL": common.HexToAddress(judge.ChainlinkSOLUSDFeed),
		"BNB": common.HexToAddress(judge.ChainlinkBNBUSDFeed),
	}
	repo := newMemoryChainlinkRoundRepo()
	repo.rounds[chainlinkXAUFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkXAUFeed, 1, start.Add(-time.Minute), 4000),
		roundAt(chainlinkXAUFeed, 2, now.Add(-time.Minute), 4040),
	}
	for _, feed := range feeds {
		repo.rounds[feed] = []chainlinkfeed.Round{
			roundAt(feed, 10, start.Add(-time.Minute), 100),
			roundAt(feed, 11, now.Add(-time.Minute), 100.5),
		}
	}
	resolver := NewChainlinkResolverWithBenchmarks(
		&fakeHistoricalFeedClient{}, repo, chainlinkXAUFeed, feeds)

	for symbol, feed := range feeds {
		t.Run(symbol, func(t *testing.T) {
			rule := v2MarketRule(judge.TypeRelative, start, end)
			rule.Benchmark, rule.BenchmarkSourceContract = symbol, feed.Hex()
			got, err := resolver.AnalyzeAt(context.Background(), rule, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.BenchmarkSymbol != symbol || got.BenchmarkCurrentPrice != 100.5 {
				t.Fatalf("%s benchmark signal is incomplete: %+v", symbol, got)
			}
		})
	}
}

func TestChainlinkSettlementSupportsEveryCreatableVersion2Template(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	end := start.Add(48 * time.Hour)
	repo := newMemoryChainlinkRoundRepo()
	repo.rounds[chainlinkXAUFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkXAUFeed, 1, start.Add(-time.Minute), 4000),
		roundAt(chainlinkXAUFeed, 2, start.Add(24*time.Hour-time.Minute), 4020),
		roundAt(chainlinkXAUFeed, 3, end.Add(-time.Minute), 4040),
	}
	repo.rounds[chainlinkBTCFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkBTCFeed, 11, start.Add(-time.Minute), 100000),
		roundAt(chainlinkBTCFeed, 12, end.Add(-time.Minute), 100200),
	}
	resolver := NewChainlinkResolver(
		&fakeHistoricalFeedClient{}, repo, chainlinkXAUFeed, chainlinkBTCFeed)

	rules := []judge.Rule{
		func() judge.Rule {
			rule := v2MarketRule(judge.TypePrice, start, end)
			rule.Direction, rule.FlatTolerance = "UP", 0.05
			return rule
		}(),
		func() judge.Rule {
			rule := v2MarketRule(judge.TypeReturnThreshold, start, end)
			rule.Operator, rule.Threshold = "GTE", 0.5
			return rule
		}(),
		func() judge.Rule {
			rule := v2MarketRule(judge.TypePriceThreshold, start, end)
			rule.Operator, rule.Threshold = "GTE", 4030
			return rule
		}(),
		func() judge.Rule {
			rule := v2MarketRule(judge.TypePriceRange, start, end)
			rule.Operator = "IN_RANGE"
			rule.LowerThreshold, rule.UpperThreshold = 4030, 4050
			return rule
		}(),
		func() judge.Rule {
			rule := v2MarketRule(judge.TypeRelative, start, end)
			rule.Benchmark, rule.BenchmarkSourceContract = "BTC", judge.ChainlinkBTCUSDFeed
			return rule
		}(),
		func() judge.Rule {
			rule := v2MarketRule(judge.TypeStreak, start, end)
			rule.Direction, rule.StreakDays = "UP", 2
			return rule
		}(),
	}

	for _, rule := range rules {
		t.Run(rule.Type, func(t *testing.T) {
			got := resolver.Resolve(context.Background(), rule)
			if !got.Determinate || got.Winner != 0 {
				t.Fatalf("settlement failed for %s: %+v", rule.Type, got)
			}
		})
	}
}
