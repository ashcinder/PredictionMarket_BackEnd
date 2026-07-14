package marketdata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/judge"
)

func TestGoldAPIClientFetchesAuditableOHLCAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatal("missing API key")
		}
		switch r.URL.Path {
		case "/ohlc/XAU":
			_, _ = w.Write([]byte(`{"open":100,"high":110,"low":95,"close":108}`))
		case "/history":
			_, _ = w.Write([]byte(`[{"hour":"2023-11-14 22","avg_price":100},{"hour":"2023-11-14 23","avg_price":105}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGoldAPIClient(server.URL, "test-key", time.Second)
	start := time.Date(2023, 11, 14, 22, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	ohlc, err := client.OHLC(context.Background(), "XAU", start, end)
	if err != nil || len(ohlc) != 1 || ohlc[0].High != 110 || ohlc[0].Low != 95 {
		t.Fatalf("OHLC = %+v, err=%v", ohlc, err)
	}
	history, err := client.History(context.Background(), "XAU", "hour", start, end)
	if err != nil || len(history) != 2 || history[1].Close != 105 || !history[1].Time.After(history[0].Time) {
		t.Fatalf("history = %+v, err=%v", history, err)
	}
}

func TestStructuredResolverFailsClosedWithoutCredentials(t *testing.T) {
	resolver := NewStructuredResolver(
		NewGoldAPIClient("https://api.gold-api.com", "", time.Second),
		NewCoinbaseClient("https://api.exchange.coinbase.com", time.Second),
	)
	result := resolver.Resolve(context.Background(), judge.Rule{
		Type: judge.TypeTouch, Symbol: "XAU", Source: "GOLD_API",
		StartTimeSec: 1, EndTimeSec: 2, Threshold: 100,
	})
	if result.Determinate || result.Winner != -1 {
		t.Fatalf("unsafe result: %+v", result)
	}
}

func TestStructuredResolverUsesCoinbaseForTwoMinuteBTCBenchmark(t *testing.T) {
	start := time.Date(2026, 7, 14, 7, 12, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)

	gold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ohlc/XAU" {
			t.Fatalf("unexpected gold path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"open":2000,"high":2003,"low":1999,"close":2002}`))
	}))
	defer gold.Close()

	coinbase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/products/BTC-USD/candles" {
			t.Fatalf("unexpected Coinbase path %s", r.URL.Path)
		}
		if r.URL.Query().Get("granularity") != "60" ||
			!strings.Contains(r.URL.Query().Get("start"), "2026-07-14T07:12:00Z") {
			t.Fatalf("unexpected Coinbase query %s", r.URL.RawQuery)
		}
		// Coinbase returns newest candle first.
		_, _ = fmt.Fprintf(w, `[[%d,101,103,101,102,8],[%d,99,101,100,101,5]]`,
			start.Add(time.Minute).Unix(), start.Unix())
	}))
	defer coinbase.Close()

	resolver := NewStructuredResolver(
		NewGoldAPIClient(gold.URL, "test-key", time.Second),
		NewCoinbaseClient(coinbase.URL, time.Second),
	)
	result := resolver.Resolve(context.Background(), judge.Rule{
		Type: judge.TypeRelative, Symbol: "XAU", Benchmark: "BTC", Source: "GOLD_API",
		StartTimeSec: start.Unix(), EndTimeSec: end.Unix(),
	})
	if !result.Determinate || result.Winner != 1 {
		t.Fatalf("gold should underperform BTC: %+v", result)
	}
	for _, expected := range []string{"XAU open 2000.000000", "return 0.100000%", "BTC open 100.000000", "return 2.000000%"} {
		if !strings.Contains(result.Summary, expected) {
			t.Fatalf("audit summary missing %q: %s", expected, result.Summary)
		}
	}
}
