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

type fakeManagedChain struct {
	wallet    string
	info      *chain.GameInfo
	extra     *chain.GameExtraData
	sendCount int
	option    int
	value     *big.Int
}

func (f *fakeManagedChain) WalletAddress() string { return f.wallet }
func (f *fakeManagedChain) Close()                {}
func (f *fakeManagedChain) GetGameInfo(context.Context, int) (*chain.GameInfo, error) {
	return f.info, nil
}
func (f *fakeManagedChain) GetGameExtraData(context.Context, int, string) (*chain.GameExtraData, error) {
	return f.extra, nil
}
func (f *fakeManagedChain) BuyShares(_ context.Context, _ int, option int, value *big.Int) (string, error) {
	f.sendCount++
	f.option = option
	f.value = new(big.Int).Set(value)
	return "0xtest", nil
}

type staticMetadata struct{ value *ipfs.Metadata }

func (s staticMetadata) DownloadMetadata(string) (*ipfs.Metadata, error) { return s.value, nil }

type staticQuote struct{ value *oracle.Quote }

func (s staticQuote) FetchQuote() (*oracle.Quote, error) { return s.value, nil }

type staticDecision struct{ value *Decision }

func (s staticDecision) Decide(context.Context, *chain.GameInfo, *chain.GameExtraData, *ipfs.Metadata, *oracle.Quote) (*Decision, error) {
	return s.value, nil
}

func newManagedTestEntry(t *testing.T) (*Store, EntrySnapshot, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	user := crypto.PubkeyToAddress(key.PublicKey).Hex()
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enable(SetRequest{
		GameID: 1, UserAddress: user, Enabled: true,
		ContractAddress: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		PrivateKey:      hexutil.Encode(crypto.FromECDSA(key)),
	}); err != nil {
		t.Fatal(err)
	}
	return store, store.Entries()[0], user
}

func newTestEngine(store *Store, client *fakeManagedChain, decision *Decision) *Engine {
	return &Engine{
		cfg:       &config.Config{AIConfidenceMin: 0.70, AIBuyAmountBKC: "2.5"},
		store:     store,
		newChain:  func(string, string) (managedChain, error) { return client, nil },
		metadata:  staticMetadata{value: &ipfs.Metadata{}},
		quotes:    staticQuote{value: &oracle.Quote{PriceUSD: 2300, QuoteSource: "test"}},
		decisions: staticDecision{value: decision},
	}
}

func TestEngineDoesNotTradeHoldOrLowConfidence(t *testing.T) {
	tests := map[string]*Decision{
		"hold":           {Action: "hold", Confidence: 1, Reason: "wait"},
		"low confidence": {Action: "buy_yes", Confidence: 0.69, Reason: "weak"},
	}
	for name, decision := range tests {
		t.Run(name, func(t *testing.T) {
			store, snapshot, user := newManagedTestEntry(t)
			client := &fakeManagedChain{
				wallet: user,
				info: &chain.GameInfo{ID: 1, TotalPool: big.NewInt(0),
					DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
				extra: &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}},
			}
			if err := newTestEngine(store, client, decision).process(context.Background(), snapshot); err != nil {
				t.Fatal(err)
			}
			if client.sendCount != 0 {
				t.Fatalf("unexpected transactions: %d", client.sendCount)
			}
		})
	}
}

func TestEngineSendsAndRecordsOneSimulatedTrade(t *testing.T) {
	store, snapshot, user := newManagedTestEntry(t)
	client := &fakeManagedChain{
		wallet: user,
		info: &chain.GameInfo{ID: 1, TotalPool: big.NewInt(0),
			DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
		extra: &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}},
	}
	engine := newTestEngine(store, client, &Decision{Action: "buy_yes", Confidence: 0.91, Reason: "strong"})
	if err := engine.process(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if client.sendCount != 1 || client.option != 0 {
		t.Fatalf("unexpected simulated sends: count=%d option=%d", client.sendCount, client.option)
	}
	expected := new(big.Int).Mul(big.NewInt(25), big.NewInt(100000000000000000))
	if client.value == nil || client.value.Cmp(expected) != 0 {
		t.Fatalf("unexpected trade value: %v", client.value)
	}
	entries := store.Entries()
	if len(entries) != 1 || entries[0].LastTradeTx != "0xtest" {
		t.Fatalf("trade was not recorded: %+v", entries)
	}
}
