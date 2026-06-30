package aimanaged

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"PredictionMarket/internal/chain"
)

const (
	maxSamplerConcurrency   = 4
	samplerCycleTimeout     = 120 * time.Second
	samplerChainCallTimeout = 15 * time.Second
	samplerFallbackTimeout  = 15 * time.Second
)

// samplerChain is the subset of chain.Client used by MarketHistorySampler.
type samplerChain interface {
	EthCall(ctx context.Context, data string) (string, error)
	WalletAddress() string
}

// SamplerCacheExt is an optional extension that the MarketHistorySampler
// calls after each successful chain sample. Implementations can write the
// data to additional cache tables (e.g. the v1 API gold_chain_states and
// gold_price_history tables).
type SamplerCacheExt interface {
	// OnSample is called after successfully reading reserves for an active game.
	OnSample(ctx context.Context, game chain.GameOnChain, reserves []*big.Int, totalPool *big.Int, yesPct, noPct float64, observedAt time.Time)
	// OnDiscover is called for every game returned by getAllGames (active or
	// not) so the cache layer can ensure a stub row exists.
	OnDiscover(ctx context.Context, game chain.GameOnChain)
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
	cacheExt        SamplerCacheExt
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

// SetCacheExt attaches an optional cache extension that is notified after
// each successful chain sample. May be nil (the default).
func (s *MarketHistorySampler) SetCacheExt(ext SamplerCacheExt) {
	s.cacheExt = ext
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
	initCtx, initCancel := context.WithTimeout(ctx, samplerCycleTimeout)
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
			cycleCtx, cycleCancel := context.WithTimeout(ctx, samplerCycleTimeout)
			s.sampleOnce(cycleCtx)
			cycleCancel()
		}
	}
}

func (s *MarketHistorySampler) sampleOnce(ctx context.Context) {
	// Step 1: Fetch all games' metadata (one eth_call).
	allGamesData := chain.EncodeGetAllGames()

	hexResult, err := s.ethCallWithTimeout(ctx, allGamesData)
	if err != nil {
		// Sampling is best-effort background work. Retrying a slow BrokerChain
		// call here can monopolize the single shared request slot and starve
		// AI post-trade position confirmation.
		slog.Warn("sampler: eth_call getAllGames failed; skipping cycle", "error", err)
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

	// Step 2: Fetch ALL games' reserves in a single batch call.
	// This avoids N per-game getGameExtraData calls and dramatically
	// reduces chain round-trips. Falls back to per-game calls if the
	// batch endpoint is unavailable (e.g. contract doesn't support it).
	var allReserves *chain.AllGamesExtraData
	extraEncoded, extraErr := chain.EncodeGetAllGamesExtraData(wallet)
	if extraErr == nil {
		extraHex, ethErr := s.ethCallWithTimeout(ctx, extraEncoded)
		if ethErr == nil {
			allReserves, err = chain.DecodeGetAllGamesExtraData(extraHex)
			if err != nil {
				slog.Warn("sampler: decode getAllGamesExtraData failed, falling back to per-game calls", "error", err)
			}
		} else {
			if errors.Is(ethErr, context.DeadlineExceeded) || errors.Is(ethErr, context.Canceled) {
				slog.Warn("sampler: batch reserve call timed out; skipping cycle to protect trade traffic", "error", ethErr)
				return
			}
			slog.Warn("sampler: eth_call getAllGamesExtraData failed, falling back to per-game calls", "error", ethErr)
		}
	}
	if ctx.Err() != nil {
		slog.Warn("sampler: cycle deadline reached before cache update", "error", ctx.Err())
		return
	}

	// Notify the cache extension about every game so stub rows are created
	// even for inactive/resolved games the frontend still needs to list.
	if s.cacheExt != nil {
		for _, game := range games {
			if ctx.Err() != nil {
				slog.Warn("sampler: cycle deadline reached during cache discovery", "error", ctx.Err())
				return
			}
			s.cacheExt.OnDiscover(ctx, game)
		}
	}

	// Step 3: Process active games.
	// When the batch call succeeded, compute prices directly from the
	// parallel arrays (O(active) without any extra chain calls).
	// Otherwise fall back to per-game getGameExtraData with limited concurrency.
	var (
		active  int
		success int
		failed  int
		mu      sync.Mutex
	)

	if allReserves != nil {
		// Fast path: batch reserves available, no more chain calls needed.
		for i, game := range games {
			if game.IsResolved || game.IsRefunded {
				continue
			}
			if chain.IsDeadlinePassed(game.DeadlineRaw, nowMillis) {
				continue
			}
			active++
			if i >= len(allReserves.ResNO) || i >= len(allReserves.ResYES) {
				mu.Lock()
				failed++
				mu.Unlock()
				slog.Warn("sampler: batch reserves index out of range", "game_id", game.ID, "index", i)
				continue
			}
			resNO := allReserves.ResNO[i]
			resYES := allReserves.ResYES[i]
			if resNO == nil || resYES == nil {
				mu.Lock()
				failed++
				mu.Unlock()
				continue
			}
			if err := s.processGameSample(ctx, game, wallet, now, resNO, resYES); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
			} else {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}

		// Populate user positions for the signer wallet from the already-
		// fetched batch data. Uses a type assertion so the SamplerCacheExt
		// interface stays clean.
		type userSharePopulator interface {
			PopulateUserShares(ctx context.Context, data *chain.AllGamesExtraData, games []chain.GameOnChain, signerAddr string)
		}
		if pop, ok := s.cacheExt.(userSharePopulator); ok {
			pop.PopulateUserShares(ctx, allReserves, games, wallet)
		}
	} else {
		// Slow path: per-game getGameExtraData calls (up to 4 concurrent).
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
				gameCtx, cancel := context.WithTimeout(ctx, samplerFallbackTimeout)
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
	}

	slog.Info("sampler: cycle complete",
		"total_games", len(games),
		"active", active,
		"success", success,
		"failed", failed,
		"batch_reserves", allReserves != nil,
	)
}

func (s *MarketHistorySampler) sampleGame(ctx context.Context, game chain.GameOnChain, wallet string, now time.Time) error {
	encoded, err := chain.EncodeGetGameExtraData(game.ID, wallet)
	if err != nil {
		slog.Warn("sampler: encode getGameExtraData failed", "game_id", game.ID, "error", err)
		return err
	}

	// Retry transient errors (504, deadline exceeded) up to 2 extra attempts
	// with a short backoff so a single slow BrokerChain cycle doesn't skip
	// every game.
	var hexResult string
	for attempt := 0; attempt < 3; attempt++ {
		hexResult, err = s.chain.EthCall(ctx, encoded)
		if err == nil {
			break
		}
		if attempt < 2 {
			backoff := time.Duration(attempt+1) * 2 * time.Second
			slog.Warn("sampler: eth_call getGameExtraData failed, retrying",
				"game_id", game.ID, "attempt", attempt+1, "backoff", backoff, "error", err)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if err != nil {
		slog.Warn("sampler: eth_call getGameExtraData failed after retries", "game_id", game.ID, "error", err)
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

	// Notify the optional v1 cache extension.
	if s.cacheExt != nil {
		totalPool := new(big.Int).Add(obs.ReserveNO, obs.ReserveYES)
		s.cacheExt.OnSample(ctx, game, extra.VirtualReservesNOYES, totalPool, obs.YesPercent, obs.NoPercent, time.Unix(obs.Time, 0))
	}

	return nil
}

func (s *MarketHistorySampler) ethCallWithTimeout(ctx context.Context, data string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, samplerChainCallTimeout)
	defer cancel()
	return s.chain.EthCall(callCtx, data)
}

// processGameSample is the fast-path variant that uses pre-fetched reserves
// from getAllGamesExtraData. Unlike sampleGame, it does NOT make any chain
// calls — it computes prices from the already-available resNO/resYES values
// and persists them directly.
func (s *MarketHistorySampler) processGameSample(
	ctx context.Context,
	game chain.GameOnChain,
	wallet string,
	now time.Time,
	resNO, resYES *big.Int,
) error {
	if resNO == nil || resYES == nil {
		return fmt.Errorf("nil reserves for game %d", game.ID)
	}

	total := new(big.Int).Add(resNO, resYES)
	if total.Sign() <= 0 {
		return fmt.Errorf("zero total reserves for game %d", game.ID)
	}

	// AMM outcome probability uses the opposite-side reserve.
	yesRat := new(big.Rat).SetFrac(resNO, total)
	noRat := new(big.Rat).SetFrac(resYES, total)
	hundred := new(big.Rat).SetInt64(100)
	yesRat.Mul(yesRat, hundred)
	noRat.Mul(noRat, hundred)
	yesPct, _ := yesRat.Float64()
	noPct, _ := noRat.Float64()

	obs := HistoryObservation{
		Time:       bucketTimestamp(now.Unix(), s.interval),
		YesPercent: yesPct,
		NoPercent:  noPct,
		ReserveNO:  new(big.Int).Set(resNO),
		ReserveYES: new(big.Int).Set(resYES),
		Source:     historySourceChain,
	}

	market := MarketIdentity{
		ContractAddress: s.contractAddress,
		GameID:          game.ID,
	}
	if _, err := s.histories.MergeAndList(ctx, market, nil, obs, s.historyMax); err != nil {
		slog.Warn("sampler: persist history failed (batch)", "game_id", game.ID, "error", err)
		return err
	}

	// Notify the optional v1 cache extension.
	if s.cacheExt != nil {
		s.cacheExt.OnSample(ctx, game, []*big.Int{resNO, resYES}, total, yesPct, noPct, time.Unix(obs.Time, 0))
	}

	return nil
}
