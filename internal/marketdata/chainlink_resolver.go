package marketdata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"PredictionMarket/internal/chainlinkfeed"
	"PredictionMarket/internal/judge"

	"github.com/ethereum/go-ethereum/common"
)

type historicalFeedReader interface {
	RoundAtOrBefore(context.Context, common.Address, time.Time) (chainlinkfeed.Round, error)
}

type boundaryRoundRepository interface {
	Save(context.Context, chainlinkfeed.Round) error
	LatestAtOrBefore(context.Context, common.Address, time.Time) (chainlinkfeed.Round, error)
}

type ChainlinkResolver struct {
	client         historicalFeedReader
	repo           boundaryRoundRepository
	xauFeed        common.Address
	benchmarkFeeds map[string]common.Address
}

func NewChainlinkResolver(
	client historicalFeedReader,
	repo boundaryRoundRepository,
	xauFeed common.Address,
	btcFeed common.Address,
) *ChainlinkResolver {
	return NewChainlinkResolverWithBenchmarks(client, repo, xauFeed, map[string]common.Address{
		"BTC": btcFeed,
	})
}

func NewChainlinkResolverWithBenchmarks(
	client historicalFeedReader,
	repo boundaryRoundRepository,
	xauFeed common.Address,
	benchmarkFeeds map[string]common.Address,
) *ChainlinkResolver {
	feeds := make(map[string]common.Address, len(benchmarkFeeds))
	for symbol, feed := range benchmarkFeeds {
		feeds[strings.ToUpper(strings.TrimSpace(symbol))] = feed
	}
	return &ChainlinkResolver{
		client: client, repo: repo, xauFeed: xauFeed, benchmarkFeeds: feeds,
	}
}

func (r *ChainlinkResolver) Resolve(ctx context.Context, rule judge.Rule) judge.Result {
	if r == nil || r.repo == nil || r.client == nil {
		return unresolved("Chainlink boundary resolver is not configured")
	}
	if err := judge.ValidateVersion2Rule(rule); err != nil {
		return unresolved("invalid version 2 rule: " + err.Error())
	}
	if r.xauFeed == (common.Address{}) || !strings.EqualFold(r.xauFeed.Hex(), rule.SourceContract) {
		return unresolved("configured XAU feed does not match source_contract")
	}

	primaryBoundaries := requiredPrimaryBoundaries(rule)
	primary, primaryAudit, err := r.loadEvidence(ctx, r.xauFeed, primaryBoundaries, time.Duration(rule.MaxStalenessSec)*time.Second)
	if err != nil {
		return unresolved(err.Error())
	}
	evidence := judge.Evidence{Primary: primary}
	auditParts := append([]string(nil), primaryAudit...)
	if rule.Type == judge.TypeRelative {
		benchmarkSymbol := strings.ToUpper(strings.TrimSpace(rule.Benchmark))
		benchmarkFeed := r.benchmarkFeeds[benchmarkSymbol]
		if benchmarkFeed == (common.Address{}) || !strings.EqualFold(benchmarkFeed.Hex(), rule.BenchmarkSourceContract) {
			return unresolved(fmt.Sprintf(
				"configured %s feed does not match benchmark_source_contract", benchmarkSymbol))
		}
		benchmark, benchmarkAudit, benchmarkErr := r.loadEvidence(
			ctx, benchmarkFeed,
			[]time.Time{time.Unix(rule.StartTimeSec, 0), time.Unix(rule.EndTimeSec, 0)},
			time.Duration(rule.MaxStalenessSec)*time.Second,
		)
		if benchmarkErr != nil {
			return unresolved("benchmark: " + benchmarkErr.Error())
		}
		evidence.Benchmark = benchmark
		auditParts = append(auditParts, benchmarkAudit...)
	}

	result := judge.EvaluateStructured(rule, evidence)
	candidate := "INDETERMINATE"
	if result.Determinate {
		if result.Winner == 0 {
			candidate = "YES"
		} else {
			candidate = "NO"
		}
	}
	result.Summary = fmt.Sprintf(
		"source=%s; %s; formula=%s; candidate=%s",
		judge.ChainlinkDataFeedEthereum, strings.Join(auditParts, "; "), result.Summary, candidate,
	)
	return result
}

func (r *ChainlinkResolver) loadEvidence(
	ctx context.Context,
	feed common.Address,
	boundaries []time.Time,
	maxStaleness time.Duration,
) ([]judge.Candle, []string, error) {
	candles := make([]judge.Candle, 0, len(boundaries))
	audit := make([]string, 0, len(boundaries))
	for _, boundary := range boundaries {
		boundary = boundary.UTC()
		cached, cacheErr := r.repo.LatestAtOrBefore(ctx, feed, boundary)
		if cacheErr != nil && !errors.Is(cacheErr, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("read cached Chainlink round for %s at %s: %w", feed.Hex(), boundary.Format(time.RFC3339), cacheErr)
		}
		round, chainErr := r.client.RoundAtOrBefore(ctx, feed, boundary)
		if chainErr == nil {
			if cached.ID == nil || cached.ID.Cmp(round.ID) != 0 {
				if saveErr := r.repo.Save(ctx, round); saveErr != nil {
					return nil, nil, fmt.Errorf("cache Chainlink round for %s at %s: %w", feed.Hex(), boundary.Format(time.RFC3339), saveErr)
				}
			}
		} else if cacheErr == nil {
			round = cached
		} else {
			return nil, nil, fmt.Errorf("load Chainlink round for %s at %s: %w", feed.Hex(), boundary.Format(time.RFC3339), chainErr)
		}
		if round.Feed != feed {
			return nil, nil, fmt.Errorf("Chainlink round feed mismatch: got %s want %s", round.Feed.Hex(), feed.Hex())
		}
		age := boundary.Sub(round.UpdatedAt.UTC())
		if age < 0 {
			return nil, nil, fmt.Errorf("Chainlink round %s is after boundary %s", round.ID, boundary.Format(time.RFC3339))
		}
		if age > maxStaleness {
			return nil, nil, fmt.Errorf(
				"Chainlink round %s is stale at boundary %s: age=%s max=%s",
				round.ID, boundary.Format(time.RFC3339), age, maxStaleness,
			)
		}
		candles = append(candles, judge.Candle{
			Time: boundary, SourceTime: round.UpdatedAt.UTC(), RoundID: round.ID.String(),
			SourceContract: feed.Hex(), Open: round.Price, High: round.Price,
			Low: round.Price, Close: round.Price,
		})
		audit = append(audit, fmt.Sprintf(
			"feed=%s boundary=%s round_id=%s source_time=%s age=%s price_usd=%.8f",
			feed.Hex(), boundary.Format(time.RFC3339), round.ID.String(),
			round.UpdatedAt.UTC().Format(time.RFC3339), age, round.Price,
		))
	}
	return candles, audit, nil
}

func requiredPrimaryBoundaries(rule judge.Rule) []time.Time {
	start := time.Unix(rule.StartTimeSec, 0).UTC()
	end := time.Unix(rule.EndTimeSec, 0).UTC()
	switch rule.Type {
	case judge.TypePriceThreshold, judge.TypePriceRange:
		return []time.Time{end}
	case judge.TypeStreak:
		boundaries := make([]time.Time, 0, rule.StreakDays+1)
		for current := start; !current.After(end); current = current.Add(24 * time.Hour) {
			boundaries = append(boundaries, current)
		}
		return boundaries
	default:
		return []time.Time{start, end}
	}
}

func unresolved(summary string) judge.Result {
	return judge.Result{Determinate: false, Winner: -1, Summary: summary}
}
