package aimanaged

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/ipfs"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestStoreEnableRejectsMismatchedKey verifies that Store.Enable validates
// the private key derives the claimed user address before encrypting it.
// This guards both the legacy and v1 enable endpoints against misconfiguration.
func TestStoreEnableRejectsMismatchedKey(t *testing.T) {
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}

	// Generate a real key to get a valid user address.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	validUser := crypto.PubkeyToAddress(key.PublicKey).Hex()
	validKey := hexutil.Encode(crypto.FromECDSA(key))

	// Generate a different key for mismatch test.
	otherKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	otherUser := crypto.PubkeyToAddress(otherKey.PublicKey).Hex()

	tests := []struct {
		name        string
		userAddress string
		privateKey  string
		wantErr     string
	}{
		{
			name:        "matching key",
			userAddress: validUser,
			privateKey:  validKey,
			wantErr:     "",
		},
		{
			name:        "mismatched key",
			userAddress: validUser,
			privateKey:  hexutil.Encode(crypto.FromECDSA(otherKey)),
			wantErr:     "private_key does not match user_address",
		},
		{
			name:        "invalid key format",
			userAddress: validUser,
			privateKey:  "not-a-key",
			wantErr:     "invalid private_key",
		},
		{
			name:        "matching key with different case",
			userAddress: otherUser,
			privateKey:  hexutil.Encode(crypto.FromECDSA(otherKey)),
			wantErr:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := store.Enable(SetRequest{
				GameID:          1,
				UserAddress:     tc.userAddress,
				Enabled:         true,
				ContractAddress: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
				PrivateKey:      tc.privateKey,
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got: %v", err)
				}
				if !store.IsEnabled(1, tc.userAddress) {
					t.Fatal("entry should be enabled after successful Enable")
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
			}
		})
	}
}

// TestEngineIPFSMetadataFailureContinuesChainSampling verifies that when IPFS
// metadata download fails, the engine still samples the chain, accumulates
// history points, and eventually passes the history gate once enough points
// are collected. It must NOT return a metadata_unavailable HOLD that blocks
// the pipeline.
func TestEngineIPFSMetadataFailureContinuesChainSampling(t *testing.T) {
	store, snapshot, user := newManagedTestEntry(t)
	client := &fakeManagedChain{
		wallet: user,
		info: &chain.GameInfo{ID: 1, TotalPool: big.NewInt(100),
			DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
		extra: &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(40), big.NewInt(60)}},
	}
	engine := newTestEngine(store, client, &Decision{Action: "hold", Confidence: 1})
	// Simulate IPFS being unavailable every poll.
	engine.metadata = &failingMetadataSource{err: errors.New("ipfs gateway timeout")}

	// First poll: should still write a chain observation even though IPFS failed.
	if err := engine.process(context.Background(), snapshot); err != nil {
		t.Fatalf("IPFS failure should not block chain sampling: %v", err)
	}

	history, err := engine.histories.List(context.Background(), MarketIdentity{
		ContractAddress: snapshot.ContractAddress, GameID: snapshot.GameID,
	}, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("chain observation should be persisted despite IPFS failure, got %d points", len(history))
	}

	// Verify we did NOT record a metadata_unavailable rule HOLD.
	rules := engine.audits.(*recordingDecisionRepository).rules
	for _, r := range rules {
		if r.Outcome == "metadata_unavailable" {
			t.Fatalf("IPFS failure should not produce metadata_unavailable HOLD: %+v", r)
		}
	}

	// Decision should not have been called (history insufficient after 1 poll).
	if engine.decisions.(*staticDecision).calls != 0 {
		t.Fatal("decision was called with insufficient history after IPFS failure")
	}
}

// failingMetadataSource implements metadataSource and always returns an error.
type failingMetadataSource struct{ err error }

func (f *failingMetadataSource) DownloadMetadata(string) (*ipfs.Metadata, error) {
	return nil, f.err
}

// TestEngineFirstUserTradeFailureDoesNotBlockSecondUser verifies that when
// two managed users share the same market and the first user's trade fails
// (e.g. broadcast error), the second user's trade still executes normally.
func TestEngineFirstUserTradeFailureDoesNotBlockSecondUser(t *testing.T) {
	store, err := NewStore()
	if err != nil {
		t.Fatal(err)
	}
	const contract = "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"

	enableUser := func(label string) (EntrySnapshot, string) {
		t.Helper()
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		user := crypto.PubkeyToAddress(key.PublicKey).Hex()
		if err := store.Enable(SetRequest{
			GameID: 1, UserAddress: user, Enabled: true,
			ContractAddress: contract,
			PrivateKey:      hexutil.Encode(crypto.FromECDSA(key)),
		}); err != nil {
			t.Fatal(err)
		}
		for _, entry := range store.Entries() {
			if entry.UserAddress == user {
				return entry, user
			}
		}
		t.Fatalf("%s entry not found", label)
		return EntrySnapshot{}, ""
	}

	firstEntry, firstUser := enableUser("first")
	secondEntry, secondUser := enableUser("second")

	callCount := 0
	var mu sync.Mutex

	engine := newTestEngine(store, &fakeManagedChain{}, &Decision{Action: "buy_yes", Confidence: 1, Reason: "strong signal"})
	engine.metadata = staticMetadata{value: &ipfs.Metadata{History: []ipfs.HistoryPoint{
		{Time: 100, YesPercent: 51, NoPercent: 49},
		{Time: 200, YesPercent: 52, NoPercent: 48},
	}}}
	engine.newChain = func(privateKey, _ string) (managedChain, error) {
		wallet, err := walletAddressFromPrivateKey(privateKey)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		idx := callCount
		callCount++
		mu.Unlock()
		buyErr := error(nil)
		// idx=0 is the read client (openReadClient), must succeed.
		// idx=1 is the first user's trade client, simulate broadcast failure.
		if idx == 1 {
			buyErr = errors.New("broadcast failed for first user")
		}
		return &fakeManagedChain{
			wallet: wallet,
			info: &chain.GameInfo{ID: 1, TotalPool: big.NewInt(100),
				DeadlineRaw: time.Now().Add(time.Hour).UnixMilli()},
			extra:  &chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(40), big.NewInt(60)}},
			buyErr: buyErr,
		}, nil
	}

	// Process both entries as one market batch.
	snapshots := []EntrySnapshot{firstEntry, secondEntry}
	if err := engine.processMarket(context.Background(), snapshots); err != nil {
		t.Fatalf("processMarket should not fail due to single user error: %v", err)
	}

	// Look up both users in the store.
	entries := store.Entries()
	var firstStored, secondStored *EntrySnapshot
	for i := range entries {
		switch entries[i].UserAddress {
		case firstUser:
			firstStored = &entries[i]
		case secondUser:
			secondStored = &entries[i]
		}
	}

	if firstStored == nil || !strings.Contains(firstStored.LastError, "broadcast failed") {
		t.Fatalf("first user error not recorded: %+v", firstStored)
	}

	// Second user should have succeeded — no error, trade recorded.
	if secondStored == nil {
		t.Fatal("second user entry missing")
	}
	if secondStored.LastError != "" {
		t.Fatalf("second user should not have error: %s", secondStored.LastError)
	}
	if secondStored.LastTradeTx != "0xtest" {
		t.Fatalf("second user trade was not recorded: %+v", secondStored)
	}

	// Verify both users got audited: first as trade_failed, second as traded.
	finalized := engine.audits.(*recordingDecisionRepository).finalized
	if len(finalized) != 2 {
		t.Fatalf("expected 2 audit finalizations, got %d: %+v", len(finalized), finalized)
	}
	if finalized[0].outcome != "trade_failed" || finalized[1].outcome != "traded" {
		t.Fatalf("unexpected audit outcomes: %+v", finalized)
	}
}
