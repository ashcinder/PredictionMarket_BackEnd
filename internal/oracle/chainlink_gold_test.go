package oracle

import (
	"context"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/chainlinkfeed"

	"github.com/ethereum/go-ethereum/common"
)

type fakeChainlinkLatestReader struct {
	round chainlinkfeed.Round
	err   error
}

type fakeChainlinkHistoricalReader struct {
	latest   chainlinkfeed.Round
	previous chainlinkfeed.Round
}

func (f fakeChainlinkHistoricalReader) LatestRound(context.Context, common.Address) (chainlinkfeed.Round, error) {
	return f.latest, nil
}

func (f fakeChainlinkHistoricalReader) RoundAtOrBefore(context.Context, common.Address, time.Time) (chainlinkfeed.Round, error) {
	return f.previous, nil
}

func (f fakeChainlinkLatestReader) LatestRound(context.Context, common.Address) (chainlinkfeed.Round, error) {
	return f.round, f.err
}

type fixedGoldPrimary struct {
	quote *Quote
	err   error
}

func (f fixedGoldPrimary) FetchQuote() (*Quote, error) { return f.quote, f.err }

func TestChainlinkGoldSourceReturnsFreshAuditableQuote(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	feed := common.HexToAddress("0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6")
	roundID := big.NewInt(42)
	source := NewChainlinkGoldSource(fakeChainlinkLatestReader{round: chainlinkfeed.Round{
		Feed: feed, ID: roundID, Answer: big.NewInt(407910500000), Decimals: 8,
		Price: 4079.105, StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-30 * time.Second),
		AnsweredInRound: new(big.Int).Set(roundID),
	}}, feed, time.Hour)
	source.now = func() time.Time { return now }
	quote, err := source.FetchQuote()
	if err != nil {
		t.Fatal(err)
	}
	if quote.PriceUSD != 4079.105 || !strings.Contains(quote.QuoteSource, "round=42") ||
		quote.QuoteUpdatedAt != now.Add(-30*time.Second).Format(time.RFC3339) {
		t.Fatalf("quote=%+v", quote)
	}
}

func TestChainlinkGoldSourceRejectsStaleQuote(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	feed := common.HexToAddress("0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6")
	roundID := big.NewInt(42)
	source := NewChainlinkGoldSource(fakeChainlinkLatestReader{round: chainlinkfeed.Round{
		Feed: feed, ID: roundID, Answer: big.NewInt(407910500000), Decimals: 8,
		Price: 4079.105, StartedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
		AnsweredInRound: new(big.Int).Set(roundID),
	}}, feed, time.Hour)
	source.now = func() time.Time { return now }
	if _, err := source.FetchQuote(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "stale") {
		t.Fatalf("error=%v", err)
	}
}

func TestChainlinkGoldSourceCalculatesAuditableDailyChange(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	feed := common.HexToAddress("0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6")
	latestID, previousID := big.NewInt(44), big.NewInt(40)
	latest := chainlinkfeed.Round{
		Feed: feed, ID: latestID, Answer: big.NewInt(404000000000), Decimals: 8,
		Price: 4040, StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-30 * time.Second),
		AnsweredInRound: new(big.Int).Set(latestID),
	}
	previous := chainlinkfeed.Round{
		Feed: feed, ID: previousID, Answer: big.NewInt(400000000000), Decimals: 8,
		Price: 4000, StartedAt: now.Add(-25 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour),
		AnsweredInRound: new(big.Int).Set(previousID),
	}
	source := NewChainlinkGoldSource(fakeChainlinkHistoricalReader{
		latest: latest, previous: previous,
	}, feed, time.Hour)
	source.now = func() time.Time { return now }
	quote, err := source.FetchQuote()
	if err != nil {
		t.Fatal(err)
	}
	if !quote.ChangeAvailable || math.Abs(quote.Change24h-1) > 1e-9 {
		t.Fatalf("quote=%+v", quote)
	}
}

func TestGoldOraclePrefersChainlinkAndFallsBackToGoldAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"price":3999,"updatedAt":"2026-07-14T09:39:31Z"}`))
	}))
	defer server.Close()
	config := Config{GoldAPIURL: server.URL, SinaURL: server.URL, RequestTimeout: time.Second}

	preferred := NewGoldOracleWithPrimary(config, fixedGoldPrimary{quote: &Quote{
		PriceUSD: 4079.105, QuoteSource: "Chainlink XAU/USD",
	}})
	quote, err := preferred.FetchQuote()
	if err != nil || quote.PriceUSD != 4079.105 {
		t.Fatalf("preferred quote=%+v err=%v", quote, err)
	}

	fallback := NewGoldOracleWithPrimary(config, fixedGoldPrimary{err: context.DeadlineExceeded})
	quote, err = fallback.FetchQuote()
	if err != nil || quote.PriceUSD != 3999 || quote.QuoteSource != "gold-api.com" {
		t.Fatalf("fallback quote=%+v err=%v", quote, err)
	}
}
