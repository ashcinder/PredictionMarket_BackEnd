package aimanaged

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"PredictionMarket/internal/chain"
)

const maxSamplerConcurrency = 4

// samplerChain is the subset of chain.Client used by MarketHistorySampler.
type samplerChain interface {
	EthCall(ctx context.Context, data string) (string, error)
	RetryableEthCall(ctx context.Context, data string) (string, error)
	WalletAddress() string
}

// MarketHistorySampler periodically reads reserves from all active games on
// chain and persists a snapshot to market_history. It runs independently of
// user activity so the chart history is always continuous.
type MarketHistorySampler struct {
	chain           samplerChain
	histories       HistoryRepository
	contractAddress string
	interval        time.Duration
	historyMax      int
}

// NewMarketHistorySampler creates a sampler that records a data point for
// every active game once per interval.
func NewMarketHistorySampler(
	chainClient samplerChain,
	histories HistoryRepository,
	contractAddress string,
	interval time.Duration,
	historyMax int,
) *MarketHistorySampler {
	return &MarketHistorySampler{
		chain:           chainClient,
		histories:       histories,
		contractAddress: contractAddress,
		interval:        interval,
		historyMax:      historyMax,
	}
}

// Run starts the sampling loop. It blocks until ctx is cancelled.
func (s *MarketHistorySampler) Run(ctx context.Context) error {
	slog.Info("market history sampler started",
		"contract", s.contractAddress,
		"wallet", s.chain.WalletAddress(),
		"interval", s.interval.String(),
	)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Sample immediately with a generous deadline for the initial cycle.
	initCtx, initCancel := context.WithTimeout(ctx, 120*time.Second)
	s.sampleOnce(initCtx)
	initCancel()

	for {
		select {
		case <-ctx.Done():
			slog.Info("market history sampler stopped")
			return ctx.Err()
		case <-ticker.C:
			// Each full cycle gets a deadline so a slow BrokerChain cannot
			// make the sampler goroutine block forever.
			cycleCtx, cycleCancel := context.WithTimeout(ctx, 120*time.Second)
			s.sampleOnce(cycleCtx)
			cycleCancel()
		}
	}
}

func (s *MarketHistorySampler) sampleOnce(ctx context.Context) {
	data := chain.EncodeGetAllGames()
	hexResult, err := s.chain.RetryableEthCall(ctx, data)
	if err != nil {
		slog.Warn("sampler: eth_call getAllGames failed", "error", err)
		return
	}
	games, err := chain.DecodeGetAllGames(hexResult)
	if err != nil {
		slog.Warn("sampler: decode getAllGames failed", "error", err)
		return
	}

	now := time.Now()
	nowMillis := now.UnixMilli()
	wallet := s.chain.WalletAddress()

	var (
		active  int
		success int
		failed  int
		mu      sync.Mutex
	)
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxSamplerConcurrency)

	for _, game := range games {
		if game.IsResolved || game.IsRefunded {
			continue
		}
		if chain.IsDeadlinePassed(game.DeadlineRaw, nowMillis) {
			continue
		}
		active++
		game := game
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			// Each game gets its own deadline so one slow call cannot
			// block the goroutine pool indefinitely.
			gameCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			if err := s.sampleGame(gameCtx, game, wallet, now); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
			} else {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	slog.Info("sampler: cycle complete",
		"total_games", len(games),
		"active", active,
		"success", success,
		"failed", failed,
	)
}

func (s *MarketHistorySampler) sampleGame(ctx context.Context, game chain.GameOnChain, wallet string, now time.Time) error {
	encoded, err := chain.EncodeGetGameExtraData(game.ID, wallet)
	if err != nil {
		slog.Warn("sampler: encode getGameExtraData failed", "game_id", game.ID, "error", err)
		return err
	}
	hexResult, err := s.chain.RetryableEthCall(ctx, encoded)
	if err != nil {
		slog.Warn("sampler: eth_call getGameExtraData failed", "game_id", game.ID, "error", err)
		return err
	}
	extra, err := chain.DecodeGetGameExtraData(hexResult)
	if err != nil {
		slog.Warn("sampler: decode getGameExtraData failed", "game_id", game.ID, "error", err)
		return err
	}

	obs, err := observationFromReserves(extra, now)
	if err != nil {
		slog.Warn("sampler: calculate observation failed", "game_id", game.ID, "error", err)
		return err
	}
	// Bucket the timestamp so that successive samples within the same
	// interval update the same row instead of creating duplicates.
	obs.Time = bucketTimestamp(obs.Time, s.interval)

	market := MarketIdentity{
		ContractAddress: s.contractAddress,
		GameID:          game.ID,
	}
	if _, err := s.histories.MergeAndList(ctx, market, nil, obs, s.historyMax); err != nil {
		slog.Warn("sampler: persist history failed", "game_id", game.ID, "error", err)
		return err
	}
	return nil
}
