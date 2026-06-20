package aimanaged

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/oracle"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
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

func TestAIManagedEndpointEnablesQueriesAndDisables(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := hexutil.Encode(crypto.FromECDSA(key))
	user := crypto.PubkeyToAddress(key.PublicKey).Hex()
	contract := "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"

	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewServer(store).Register(mux)

	post := func(enabled bool, key string) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(SetRequest{
			GameID: 1, UserAddress: user, Enabled: enabled,
			ContractAddress: contract, PrivateKey: key,
		})
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost,
			"/api/gold/ai-managed", bytes.NewReader(payload)))
		return recorder
	}
	get := func() bool {
		t.Helper()
		recorder := httptest.NewRecorder()
		target := "/api/gold/ai-managed?game_id=1&user_address=" + url.QueryEscape(user)
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		var response map[string]bool
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response["enabled"]
	}

	if response := post(true, privateKey); response.Code != http.StatusOK {
		t.Fatalf("enable failed: status=%d body=%s", response.Code, response.Body.String())
	}
	if !get() {
		t.Fatal("managed entry was not enabled")
	}
	if response := post(false, ""); response.Code != http.StatusOK {
		t.Fatalf("disable failed: status=%d body=%s", response.Code, response.Body.String())
	}
	if get() {
		t.Fatal("managed entry was not disabled")
	}
}
