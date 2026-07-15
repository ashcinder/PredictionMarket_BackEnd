package marketdata

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"PredictionMarket/internal/chainlinkfeed"

	"github.com/ethereum/go-ethereum/common"
)

type fakeLatestRoundClient struct {
	mu     sync.Mutex
	rounds map[common.Address]chainlinkfeed.Round
}

func (f *fakeLatestRoundClient) LatestRound(_ context.Context, feed common.Address) (chainlinkfeed.Round, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rounds[feed], nil
}

type countingChainlinkRoundRepository struct {
	mu    sync.Mutex
	saves []chainlinkfeed.Round
}

func (r *countingChainlinkRoundRepository) Save(_ context.Context, round chainlinkfeed.Round) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves = append(r.saves, round)
	return nil
}

func TestChainlinkRoundRecorderStoresOnlyNewLatestRounds(t *testing.T) {
	round := fixtureChainlinkRound()
	client := &fakeLatestRoundClient{rounds: map[common.Address]chainlinkfeed.Round{
		chainlinkXAUFeed: round,
	}}
	repo := &countingChainlinkRoundRepository{}
	recorder := NewChainlinkRoundRecorder(client, repo, []common.Address{chainlinkXAUFeed}, time.Minute)

	recorder.recordOnce(context.Background())
	recorder.recordOnce(context.Background())
	if len(repo.saves) != 1 {
		t.Fatalf("saves = %d, want 1", len(repo.saves))
	}

	nextID := new(big.Int).Add(round.ID, big.NewInt(1))
	next := round
	next.ID = nextID
	next.AnsweredInRound = new(big.Int).Set(nextID)
	client.mu.Lock()
	client.rounds[chainlinkXAUFeed] = next
	client.mu.Unlock()
	recorder.recordOnce(context.Background())
	if len(repo.saves) != 2 {
		t.Fatalf("saves after next round = %d, want 2", len(repo.saves))
	}
}
