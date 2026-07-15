package oracle

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"PredictionMarket/internal/chainlinkfeed"

	"github.com/ethereum/go-ethereum/common"
)

type chainlinkLatestRoundReader interface {
	LatestRound(context.Context, common.Address) (chainlinkfeed.Round, error)
}

type chainlinkHistoricalRoundReader interface {
	RoundAtOrBefore(context.Context, common.Address, time.Time) (chainlinkfeed.Round, error)
}

type ChainlinkGoldSource struct {
	client       chainlinkLatestRoundReader
	feed         common.Address
	maxStaleness time.Duration
	now          func() time.Time
}

func NewChainlinkGoldSource(
	client chainlinkLatestRoundReader, feed common.Address, maxStaleness time.Duration,
) *ChainlinkGoldSource {
	return &ChainlinkGoldSource{
		client: client, feed: feed, maxStaleness: maxStaleness, now: time.Now,
	}
}

func (s *ChainlinkGoldSource) FetchQuote() (*Quote, error) {
	if s == nil || s.client == nil || s.feed == (common.Address{}) || s.maxStaleness <= 0 {
		return nil, fmt.Errorf("Chainlink XAU/USD source is not configured")
	}
	round, err := s.client.LatestRound(context.Background(), s.feed)
	if err != nil {
		return nil, fmt.Errorf("read Chainlink XAU/USD: %w", err)
	}
	if round.Feed != s.feed || round.ID == nil || round.Price <= 0 ||
		math.IsNaN(round.Price) || math.IsInf(round.Price, 0) || round.UpdatedAt.IsZero() {
		return nil, fmt.Errorf("Chainlink XAU/USD round is invalid")
	}
	age := s.now().UTC().Sub(round.UpdatedAt.UTC())
	if age < 0 || age > s.maxStaleness {
		return nil, fmt.Errorf("Chainlink XAU/USD quote is stale: age=%s max=%s", age, s.maxStaleness)
	}
	quote := &Quote{
		PriceUSD: round.Price,
		QuoteSource: fmt.Sprintf("Chainlink XAU/USD feed=%s round=%s",
			round.Feed.Hex(), round.ID.String()),
		QuoteUpdatedAt: round.UpdatedAt.UTC().Format(time.RFC3339),
	}
	// The header promises a daily movement. Do not turn missing history into a
	// misleading +0.00%: expose availability separately and only calculate when
	// the same Chainlink feed can supply an auditable round 24 hours earlier.
	if history, ok := s.client.(chainlinkHistoricalRoundReader); ok {
		previous, previousErr := history.RoundAtOrBefore(
			context.Background(), s.feed, round.UpdatedAt.UTC().Add(-24*time.Hour))
		if previousErr == nil && previous.Feed == s.feed && previous.Price > 0 &&
			!math.IsNaN(previous.Price) && !math.IsInf(previous.Price, 0) {
			quote.Change24h = (round.Price - previous.Price) / previous.Price * 100
			quote.ChangeAvailable = true
		}
	}
	return quote, nil
}

func isChainlinkQuote(source string) bool {
	return strings.HasPrefix(strings.TrimSpace(source), "Chainlink XAU/USD")
}
