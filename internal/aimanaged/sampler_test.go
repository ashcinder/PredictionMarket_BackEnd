package aimanaged

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"PredictionMarket/internal/chain"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// ---------- test helpers for ABI-encoded chain responses ----------

const samplerTestABI = `[
{"constant":true,"inputs":[],"name":"getAllGames","outputs":[{"name":"ids","type":"uint256[]"},{"name":"cids","type":"string[]"},{"name":"pools","type":"uint256[]"},{"name":"deadlines","type":"uint256[]"},{"name":"resolved","type":"bool[]"},{"name":"refunded","type":"bool[]"},{"name":"winners","type":"uint8[]"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":true,"inputs":[{"name":"id","type":"uint256"},{"name":"user","type":"address"}],"name":"getGameExtraData","outputs":[{"name":"virtualReserves","type":"uint256[]"},{"name":"myShares","type":"uint256[]"}],"payable":false,"stateMutability":"view","type":"function"}
]`

var samplerParsedABI abi.ABI

func init() {
	var err error
	samplerParsedABI, err = abi.JSON(strings.NewReader(samplerTestABI))
	if err != nil {
		panic("sampler test ABI: " + err.Error())
	}
}

func encodeGetAllGamesResult(games []chain.GameOnChain) string {
	ids := make([]*big.Int, len(games))
	cids := make([]string, len(games))
	pools := make([]*big.Int, len(games))
	deadlines := make([]*big.Int, len(games))
	resolved := make([]bool, len(games))
	refunded := make([]bool, len(games))
	winners := make([]uint8, len(games))
	for i, g := range games {
		ids[i] = big.NewInt(int64(g.ID))
		cids[i] = g.IPFSCID
		pools[i] = g.TotalPool
		deadlines[i] = big.NewInt(g.DeadlineRaw)
		resolved[i] = g.IsResolved
		refunded[i] = g.IsRefunded
		winners[i] = uint8(g.WinningOption)
	}
	method, ok := samplerParsedABI.Methods["getAllGames"]
	if !ok {
		panic("getAllGames not found in test ABI")
	}
	packed, err := method.Outputs.Pack(ids, cids, pools, deadlines, resolved, refunded, winners)
	if err != nil {
		panic("encode getAllGames: " + err.Error())
	}
	return "0x" + hex.EncodeToString(packed)
}

func encodeGetGameExtraDataResult(extra *chain.GameExtraData) string {
	method, ok := samplerParsedABI.Methods["getGameExtraData"]
	if !ok {
		panic("getGameExtraData not found in test ABI")
	}
	packed, err := method.Outputs.Pack(extra.VirtualReservesNOYES, extra.MySharesYESNO)
	if err != nil {
		panic("encode getGameExtraData: " + err.Error())
	}
	return "0x" + hex.EncodeToString(packed)
}

// ---------- mocks ----------

type mockSamplerChain struct {
	wallet    string
	ethCallFn func(ctx context.Context, data string) (string, error)
	// record calls for assertions
	mu    sync.Mutex
	calls []string
}

func (m *mockSamplerChain) EthCall(ctx context.Context, data string) (string, error) {
	m.mu.Lock()
	m.calls = append(m.calls, data)
	m.mu.Unlock()
	if m.ethCallFn != nil {
		return m.ethCallFn(ctx, data)
	}
	return "0x", nil
}

func (m *mockSamplerChain) RetryableEthCall(ctx context.Context, data string) (string, error) {
	return m.EthCall(ctx, data)
}

func (m *mockSamplerChain) WalletAddress() string { return m.wallet }

func (m *mockSamplerChain) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mergeAndListCall struct {
	market  MarketIdentity
	seedLen int
	current HistoryObservation
	limit   int
}

type mockSamplerHistory struct {
	mu    sync.Mutex
	calls []mergeAndListCall
	err   error
}

func (m *mockSamplerHistory) MergeAndList(ctx context.Context, market MarketIdentity, seed []HistoryObservation, current HistoryObservation, limit int) ([]HistoryObservation, error) {
	m.mu.Lock()
	m.calls = append(m.calls, mergeAndListCall{market: market, seedLen: len(seed), current: current, limit: limit})
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return []HistoryObservation{current}, nil
}

func (m *mockSamplerHistory) List(ctx context.Context, market MarketIdentity, limit int) ([]HistoryObservation, error) {
	return nil, nil
}

func (m *mockSamplerHistory) mergeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// ---------- helpers ----------

func activeGame(id int) chain.GameOnChain {
	return chain.GameOnChain{
		ID:          id,
		IPFSCID:     "QmTest",
		TotalPool:   big.NewInt(1000),
		DeadlineRaw: time.Now().Add(24 * time.Hour).Unix(),
		IsResolved:  false,
		IsRefunded:  false,
	}
}

func resolvedGame(id int) chain.GameOnChain {
	return chain.GameOnChain{
		ID:          id,
		IPFSCID:     "QmResolved",
		TotalPool:   big.NewInt(500),
		DeadlineRaw: time.Now().Add(-1 * time.Hour).Unix(),
		IsResolved:  true,
		IsRefunded:  false,
	}
}

func refundedGame(id int) chain.GameOnChain {
	return chain.GameOnChain{
		ID:          id,
		IPFSCID:     "QmRefunded",
		TotalPool:   big.NewInt(500),
		DeadlineRaw: time.Now().Add(24 * time.Hour).Unix(),
		IsResolved:  false,
		IsRefunded:  true,
	}
}

func pastDeadlineGame(id int) chain.GameOnChain {
	return chain.GameOnChain{
		ID:          id,
		IPFSCID:     "QmExpired",
		TotalPool:   big.NewInt(300),
		DeadlineRaw: time.Now().Add(-2 * time.Hour).UnixMilli() / 1000,
		IsResolved:  false,
		IsRefunded:  false,
	}
}

func validExtraData() *chain.GameExtraData {
	return &chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{big.NewInt(400), big.NewInt(600)},
		MySharesYESNO:        []*big.Int{big.NewInt(0), big.NewInt(0)},
	}
}

// ---------- tests ----------

func TestNewMarketHistorySampler(t *testing.T) {
	chain := &mockSamplerChain{wallet: "0x1111111111111111111111111111111111111111"}
	histories := &mockSamplerHistory{}
	sampler := NewMarketHistorySampler(chain, histories, "0xabc", time.Minute, 256)
	if sampler.contractAddress != "0xabc" {
		t.Fatalf("unexpected contract address: %q", sampler.contractAddress)
	}
	if sampler.interval != time.Minute {
		t.Fatalf("unexpected interval: %s", sampler.interval)
	}
	if sampler.historyMax != 256 {
		t.Fatalf("unexpected history max: %d", sampler.historyMax)
	}
}

func TestSamplerSkipsResolvedRefundedAndPastDeadlineGames(t *testing.T) {
	games := []chain.GameOnChain{
		activeGame(1),
		resolvedGame(2),
		refundedGame(3),
		pastDeadlineGame(4),
	}
	chainMock := &mockSamplerChain{
		wallet: "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) {
			if len(data) > 100 {
				// getGameExtraData call (selector + gameID + address = 136+ chars)
				return encodeGetGameExtraDataResult(validExtraData()), nil
			}
			// getAllGames call
			return encodeGetAllGamesResult(games), nil
		},
	}
	histories := &mockSamplerHistory{}
	sampler := NewMarketHistorySampler(chainMock, histories, "0xContract", time.Minute, 256)

	sampler.sampleOnce(context.Background())

	// Only game 1 should be sampled
	if n := histories.mergeCount(); n != 1 {
		t.Fatalf("expected 1 history merge, got %d", n)
	}
	if histories.calls[0].market.GameID != 1 {
		t.Fatalf("expected game 1 to be sampled, got game %d", histories.calls[0].market.GameID)
	}
	// getAllGames (1) + getGameExtraData for game 1 only (1) = 2
	if c := chainMock.callCount(); c != 2 {
		t.Fatalf("expected 2 eth_call invocations, got %d", c)
	}
}

func TestSamplerCalculatesYesNoPercentFromReserves(t *testing.T) {
	// reserveNO=300, reserveYES=700 → yes=70%, no=30%
	extra := &chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{big.NewInt(300), big.NewInt(700)},
		MySharesYESNO:        []*big.Int{big.NewInt(0), big.NewInt(0)},
	}
	chainMock := &mockSamplerChain{
		wallet: "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) {
			if strings.HasPrefix(data, "0x") && len(data) > 20 {
				return encodeGetGameExtraDataResult(extra), nil
			}
			return encodeGetAllGamesResult([]chain.GameOnChain{activeGame(1)}), nil
		},
	}
	histories := &mockSamplerHistory{}
	sampler := NewMarketHistorySampler(chainMock, histories, "0xContract", time.Minute, 256)

	sampler.sampleOnce(context.Background())

	if n := histories.mergeCount(); n != 1 {
		t.Fatalf("expected 1 history merge, got %d", n)
	}
	obs := histories.calls[0].current
	if obs.YesPercent < 69 || obs.YesPercent > 71 {
		t.Fatalf("expected yes_percent ~70%%, got %.4f", obs.YesPercent)
	}
	if obs.NoPercent < 29 || obs.NoPercent > 31 {
		t.Fatalf("expected no_percent ~30%%, got %.4f", obs.NoPercent)
	}
	if obs.Source != historySourceChain {
		t.Fatalf("expected chain source, got %q", obs.Source)
	}
	if obs.ReserveNO == nil || obs.ReserveYES == nil {
		t.Fatal("expected reserves to be set")
	}
	if obs.ReserveNO.Cmp(big.NewInt(300)) != 0 || obs.ReserveYES.Cmp(big.NewInt(700)) != 0 {
		t.Fatalf("unexpected reserve values: NO=%s YES=%s", obs.ReserveNO, obs.ReserveYES)
	}
}

func TestSamplerContinuesOnGameFailure(t *testing.T) {
	games := []chain.GameOnChain{activeGame(1), activeGame(2), activeGame(3)}
	chainMock := &mockSamplerChain{
		wallet: "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) {
			if len(data) > 100 {
				// getGameExtraData call — game 2 returns empty
				if strings.Contains(data, encodeGameID(2)) {
					return "0x", nil
				}
				return encodeGetGameExtraDataResult(validExtraData()), nil
			}
			return encodeGetAllGamesResult(games), nil
		},
	}
	histories := &mockSamplerHistory{}
	sampler := NewMarketHistorySampler(chainMock, histories, "0xContract", time.Minute, 256)

	sampler.sampleOnce(context.Background())

	// Games 1 and 3 should be sampled; game 2 fails silently
	if n := histories.mergeCount(); n != 2 {
		t.Fatalf("expected 2 history merges (game 2 failed), got %d", n)
	}
}

func TestSamplerDoesNotCrashWhenGetAllGamesFails(t *testing.T) {
	chainMock := &mockSamplerChain{
		wallet: "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) {
			return "", nil
		},
	}
	histories := &mockSamplerHistory{}
	sampler := NewMarketHistorySampler(chainMock, histories, "0xContract", time.Minute, 256)

	// Must not panic
	sampler.sampleOnce(context.Background())

	if n := histories.mergeCount(); n != 0 {
		t.Fatalf("expected 0 merges when getAllGames fails, got %d", n)
	}
}

func TestSamplerStopsOnContextCancellation(t *testing.T) {
	chainMock := &mockSamplerChain{
		wallet: "0x1111111111111111111111111111111111111111",
		ethCallFn: func(ctx context.Context, data string) (string, error) {
			return encodeGetAllGamesResult([]chain.GameOnChain{activeGame(1)}), nil
		},
	}
	histories := &mockSamplerHistory{}
	sampler := NewMarketHistorySampler(chainMock, histories, "0xContract", time.Minute, 256)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sampler.Run(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func encodeGameID(id int) string {
	word := make([]byte, 32)
	b := big.NewInt(int64(id)).Bytes()
	copy(word[32-len(b):], b)
	return hex.EncodeToString(word)
}
