package apiv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"PredictionMarket/internal/aimanaged"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// newAITestServer creates a v1 Server wired only with an aiStore for
// testing the /api/v1/gold/ai-managed endpoints.
func newAITestServer(t *testing.T) (*Server, *aimanaged.Store) {
	t.Helper()
	store, err := aimanaged.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, nil, nil, nil, nil, store, nil, nil, "0xContract", 256)
	return srv, store
}

// TestV1AIManagedMatchingKeyEnableSuccess verifies that POST to
// /api/v1/gold/ai-managed with a private key that derives the claimed
// user_address returns 200, and a subsequent GET returns enabled=true.
func TestV1AIManagedMatchingKeyEnableSuccess(t *testing.T) {
	srv, _ := newAITestServer(t)
	mux := http.NewServeMux()
	srv.Register(mux)

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := hexutil.Encode(crypto.FromECDSA(key))
	userAddress := crypto.PubkeyToAddress(key.PublicKey).Hex()
	contractAddress := "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"

	// POST enable
	postBody, err := json.Marshal(aimanaged.SetRequest{
		GameID:          1,
		UserAddress:     userAddress,
		Enabled:         true,
		ContractAddress: contractAddress,
		PrivateKey:      privateKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	postRec := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/gold/ai-managed", bytes.NewReader(postBody))
	mux.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("POST enable: expected 200, got %d body=%s", postRec.Code, postRec.Body.String())
	}

	var postResp map[string]bool
	if err := json.NewDecoder(postRec.Body).Decode(&postResp); err != nil {
		t.Fatal(err)
	}
	if !postResp["success"] {
		t.Fatalf("POST enable: expected success=true, got %+v", postResp)
	}

	// GET check enabled
	getRec := httptest.NewRecorder()
	getURL := "/api/v1/gold/ai-managed?game_id=1&user_address=" + url.QueryEscape(userAddress)
	getReq := httptest.NewRequest(http.MethodGet, getURL, nil)
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET enabled: expected 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	var getResp map[string]bool
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatal(err)
	}
	if !getResp["enabled"] {
		t.Fatalf("GET enabled: expected enabled=true, got %+v", getResp)
	}
}

// TestV1AIManagedMismatchedKeyReturns400AndStaysDisabled verifies that POST to
// /api/v1/gold/ai-managed with a private key that does NOT derive the claimed
// user_address returns 400, and a subsequent GET returns enabled=false.
func TestV1AIManagedMismatchedKeyReturns400AndStaysDisabled(t *testing.T) {
	srv, _ := newAITestServer(t)
	mux := http.NewServeMux()
	srv.Register(mux)

	// Generate key for user A
	keyA, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	userAddressA := crypto.PubkeyToAddress(keyA.PublicKey).Hex()

	// Generate key for user B (used as the mismatched private key)
	keyB, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKeyB := hexutil.Encode(crypto.FromECDSA(keyB))

	contractAddress := "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"

	// POST enable with mismatched key (user_address=A, private_key=B)
	postBody, err := json.Marshal(aimanaged.SetRequest{
		GameID:          1,
		UserAddress:     userAddressA,
		Enabled:         true,
		ContractAddress: contractAddress,
		PrivateKey:      privateKeyB,
	})
	if err != nil {
		t.Fatal(err)
	}

	postRec := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/gold/ai-managed", bytes.NewReader(postBody))
	mux.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusBadRequest {
		t.Fatalf("POST mismatched key: expected 400, got %d body=%s", postRec.Code, postRec.Body.String())
	}

	var postErr map[string]string
	if err := json.NewDecoder(postRec.Body).Decode(&postErr); err != nil {
		t.Fatal(err)
	}
	if postErr["error"] == "" {
		t.Fatal("POST mismatched key: expected error message in response")
	}

	// GET should show disabled — mismatched key must not have enabled the entry.
	getRec := httptest.NewRecorder()
	getURL := "/api/v1/gold/ai-managed?game_id=1&user_address=" + url.QueryEscape(userAddressA)
	getReq := httptest.NewRequest(http.MethodGet, getURL, nil)
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET after mismatch: expected 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	var getResp map[string]bool
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatal(err)
	}
	if getResp["enabled"] {
		t.Fatalf("GET after mismatch: expected enabled=false, got %+v", getResp)
	}
}
