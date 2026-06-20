package aimanaged

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/oracle"
)

func TestAIClientUsesConfiguredKeyAndModel(t *testing.T) {
	var authorization string
	var model string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		model = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"hold\",\"confidence\":0.4,\"reason\":\"test\"}"}}]}`))
	}))
	defer server.Close()

	t.Setenv("DEEPSEEK_API_KEY", "environment-key-must-not-win")
	client := NewAIClient(&config.Config{
		AIAPIKey:  "yaml-key",
		AIBaseURL: server.URL,
		AIModel:   "yaml-model",
	})
	decision, err := client.Decide(context.Background(),
		&chain.GameInfo{ID: 1, TotalPool: big.NewInt(0), DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
		&chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}},
		&ipfs.Metadata{},
		&oracle.Quote{PriceUSD: 2300, QuoteSource: "test"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer yaml-key" || model != "yaml-model" {
		t.Fatalf("AI request used wrong config: auth=%q model=%q", authorization, model)
	}
	if decision.Action != "hold" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}
