package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	resolver := NewStructuredResolver(NewGoldAPIClient("https://api.gold-api.com", "", time.Second))
	result := resolver.Resolve(context.Background(), judge.Rule{
		Type: judge.TypeTouch, Symbol: "XAU", Source: "GOLD_API",
		StartTimeSec: 1, EndTimeSec: 2, Threshold: 100,
	})
	if result.Determinate || result.Winner != -1 {
		t.Fatalf("unsafe result: %+v", result)
	}
}
