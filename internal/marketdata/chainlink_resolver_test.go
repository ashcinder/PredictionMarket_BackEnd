package marketdata

import (
	"context"
	"database/sql"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"PredictionMarket/internal/chainlinkfeed"
	"PredictionMarket/internal/judge"

	"github.com/ethereum/go-ethereum/common"
)

var chainlinkBTCFeed = common.HexToAddress(judge.ChainlinkBTCUSDFeed)

type memoryChainlinkRoundRepo struct {
	mu        sync.Mutex
	rounds    map[common.Address][]chainlinkfeed.Round
	saveCount int
}

func newMemoryChainlinkRoundRepo() *memoryChainlinkRoundRepo {
	return &memoryChainlinkRoundRepo{rounds: make(map[common.Address][]chainlinkfeed.Round)}
}

func (r *memoryChainlinkRoundRepo) Save(_ context.Context, round chainlinkfeed.Round) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rounds[round.Feed] = append(r.rounds[round.Feed], round)
	r.saveCount++
	return nil
}

func (r *memoryChainlinkRoundRepo) LatestAtOrBefore(_ context.Context, feed common.Address, boundary time.Time) (chainlinkfeed.Round, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found chainlinkfeed.Round
	for _, round := range r.rounds[feed] {
		if !round.UpdatedAt.After(boundary) && (found.ID == nil || round.UpdatedAt.After(found.UpdatedAt)) {
			found = round
		}
	}
	if found.ID == nil {
		return chainlinkfeed.Round{}, sql.ErrNoRows
	}
	return found, nil
}

type fakeHistoricalFeedClient struct {
	rounds map[common.Address][]chainlinkfeed.Round
}

func (f *fakeHistoricalFeedClient) RoundAtOrBefore(_ context.Context, feed common.Address, boundary time.Time) (chainlinkfeed.Round, error) {
	var found chainlinkfeed.Round
	for _, round := range f.rounds[feed] {
		if !round.UpdatedAt.After(boundary) && (found.ID == nil || round.UpdatedAt.After(found.UpdatedAt)) {
			found = round
		}
	}
	if found.ID == nil {
		return chainlinkfeed.Round{}, sql.ErrNoRows
	}
	return found, nil
}

func TestChainlinkResolverRecoversMissingBoundaryRoundsAndCachesThem(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	end := start.Add(24 * time.Hour)
	repo := newMemoryChainlinkRoundRepo()
	client := &fakeHistoricalFeedClient{rounds: map[common.Address][]chainlinkfeed.Round{
		chainlinkXAUFeed: {roundAt(chainlinkXAUFeed, 1, start.Add(-time.Minute), 4000), roundAt(chainlinkXAUFeed, 2, end.Add(-time.Minute), 4040)},
	}}
	resolver := NewChainlinkResolver(client, repo, chainlinkXAUFeed, chainlinkBTCFeed)
	got := resolver.Resolve(context.Background(), v2MarketRule(judge.TypePrice, start, end))
	if !got.Determinate || got.Winner != 0 || repo.saveCount != 2 {
		t.Fatalf("result=%+v saves=%d", got, repo.saveCount)
	}
	if !strings.Contains(got.Summary, "round_id=1") || !strings.Contains(got.Summary, "candidate=YES") {
		t.Fatalf("audit summary is incomplete: %s", got.Summary)
	}
}

func TestChainlinkResolverRejectsStaleBoundaryRound(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	end := start.Add(24 * time.Hour)
	repo := newMemoryChainlinkRoundRepo()
	repo.rounds[chainlinkXAUFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkXAUFeed, 1, start.Add(-13*time.Hour), 4000),
		roundAt(chainlinkXAUFeed, 2, end.Add(-time.Minute), 4040),
	}
	resolver := NewChainlinkResolver(&fakeHistoricalFeedClient{}, repo, chainlinkXAUFeed, chainlinkBTCFeed)
	got := resolver.Resolve(context.Background(), v2MarketRule(judge.TypePrice, start, end))
	if got.Determinate || !strings.Contains(strings.ToLower(got.Summary), "stale") {
		t.Fatalf("result=%+v", got)
	}
}

func TestChainlinkResolverEvaluatesRelativeAndStreakMarkets(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	repo := newMemoryChainlinkRoundRepo()
	for i, price := range []float64{4000, 4010, 4020, 4030} {
		boundary := start.Add(time.Duration(i) * 24 * time.Hour)
		repo.rounds[chainlinkXAUFeed] = append(repo.rounds[chainlinkXAUFeed],
			roundAt(chainlinkXAUFeed, int64(i+1), boundary.Add(-time.Minute), price))
	}
	for i, price := range []float64{100000, 100100} {
		boundary := start.Add(time.Duration(i) * 24 * time.Hour)
		repo.rounds[chainlinkBTCFeed] = append(repo.rounds[chainlinkBTCFeed],
			roundAt(chainlinkBTCFeed, int64(i+10), boundary.Add(-time.Minute), price))
	}
	resolver := NewChainlinkResolver(&fakeHistoricalFeedClient{}, repo, chainlinkXAUFeed, chainlinkBTCFeed)
	relative := v2MarketRule(judge.TypeRelative, start, start.Add(24*time.Hour))
	relative.Benchmark, relative.BenchmarkSourceContract = "BTC", judge.ChainlinkBTCUSDFeed
	if got := resolver.Resolve(context.Background(), relative); !got.Determinate || got.Winner != 0 {
		t.Fatalf("relative result=%+v", got)
	}
	streak := v2MarketRule(judge.TypeStreak, start, start.Add(72*time.Hour))
	streak.Direction, streak.StreakDays = "UP", 3
	if got := resolver.Resolve(context.Background(), streak); !got.Determinate || got.Winner != 0 {
		t.Fatalf("streak result=%+v", got)
	}
}

func TestChainlinkResolverEvaluatesETHRelativeMarket(t *testing.T) {
	start := marketBeijingBoundary(2026, time.July, 14)
	end := start.Add(24 * time.Hour)
	ethFeed := common.HexToAddress(judge.ChainlinkETHUSDFeed)
	repo := newMemoryChainlinkRoundRepo()
	repo.rounds[chainlinkXAUFeed] = []chainlinkfeed.Round{
		roundAt(chainlinkXAUFeed, 1, start.Add(-time.Minute), 4000),
		roundAt(chainlinkXAUFeed, 2, end.Add(-time.Minute), 4040),
	}
	repo.rounds[ethFeed] = []chainlinkfeed.Round{
		roundAt(ethFeed, 11, start.Add(-time.Minute), 2000),
		roundAt(ethFeed, 12, end.Add(-time.Minute), 2010),
	}
	resolver := NewChainlinkResolverWithBenchmarks(
		&fakeHistoricalFeedClient{}, repo, chainlinkXAUFeed,
		map[string]common.Address{"ETH": ethFeed},
	)
	rule := v2MarketRule(judge.TypeRelative, start, end)
	rule.Benchmark, rule.BenchmarkSourceContract = "ETH", judge.ChainlinkETHUSDFeed
	got := resolver.Resolve(context.Background(), rule)
	if !got.Determinate || got.Winner != 0 || !strings.Contains(got.Summary, ethFeed.Hex()) {
		t.Fatalf("result=%+v", got)
	}
}

func roundAt(feed common.Address, id int64, sourceTime time.Time, price float64) chainlinkfeed.Round {
	raw := big.NewInt(int64(price * 1e8))
	roundID := big.NewInt(id)
	return chainlinkfeed.Round{
		Feed: feed, ID: roundID, Answer: raw, Decimals: 8, Price: price,
		StartedAt: sourceTime.Add(-time.Second), UpdatedAt: sourceTime,
		AnsweredInRound: new(big.Int).Set(roundID),
	}
}

func v2MarketRule(typ string, start, end time.Time) judge.Rule {
	rule := judge.Rule{
		RuleVersion: 2, Type: typ, Symbol: "XAU", Source: judge.ChainlinkDataFeedEthereum,
		SourceContract: judge.ChainlinkXAUUSDFeed, Timezone: judge.BeijingTimezone,
		BoundaryPolicy: judge.LastAtOrBefore, MaxStalenessSec: 43200,
		StartTimeSec: start.Unix(), EndTimeSec: end.Unix(),
	}
	if typ == judge.TypePrice {
		rule.Direction, rule.FlatTolerance = "UP", 0.05
	}
	return rule
}

func marketBeijingBoundary(year int, month time.Month, day int) time.Time {
	location, _ := time.LoadLocation(judge.BeijingTimezone)
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}
