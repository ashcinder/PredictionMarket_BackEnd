package apiv1

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"PredictionMarket/internal/aimanaged"
	"PredictionMarket/internal/chain"
)

// samplerCacheExt implements aimanaged.SamplerCacheExt. After each successful
// chain sample by MarketHistorySampler it writes the latest reserves and
// price data to the v1 cache tables (gold_chain_states, gold_price_history),
// keeping the DApp cache fresh without depending on explicit DApp sync calls.
//
// It also auto-creates a minimal gold_games row when a game is seen on chain
// for the first time, so that the FK-free chain_states and price_history
// inserts always succeed. Full metadata (desc, condition, etc.) is filled in
// later when the DApp calls POST /api/v1/gold/games/sync.
type samplerCacheExt struct {
	games        GameMetadataRepository
	chainStates  ChainStateRepository
	history      PriceHistoryRepository
	positions    UserPositionRepository
	contractAddr string
}

// NewSamplerCacheExt creates a SamplerCacheExt that writes to the v1 cache
// tables. Pass the same *MySQLRepository for all four arguments.
func NewSamplerCacheExt(
	games GameMetadataRepository,
	chainStates ChainStateRepository,
	history PriceHistoryRepository,
	positions UserPositionRepository,
	contractAddr string,
) aimanaged.SamplerCacheExt {
	return &samplerCacheExt{
		games:        games,
		chainStates:  chainStates,
		history:      history,
		positions:    positions,
		contractAddr: contractAddr,
	}
}

// OnDiscover is called for every game returned by getAllGames (active or not).
// It ensures a stub row exists in gold_games and a basic chain state row
// (without reserves, which require a separate getGameExtraData call).
func (e *samplerCacheExt) OnDiscover(ctx context.Context, game chain.GameOnChain) {
	// 1. Ensure gold_games stub row exists.
	e.ensureGameExists(ctx, game)

	// 2. Upsert basic chain state (all fields from getAllGames, no reserves).
	state := &chainStateRow{
		GameID:          game.ID,
		ContractAddress: e.contractAddr,
		TotalPool:       game.TotalPool,
		IsResolved:      game.IsResolved,
		IsRefunded:      game.IsRefunded,
		WinningOption:   game.WinningOption,
		DeadlineSec:     game.DeadlineRaw,
		UpdatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := e.chainStates.UpsertChainState(ctx, state); err != nil {
		slog.Warn("apiv1: sampler ext discover upsert chain state failed", "game_id", game.ID, "error", err)
	}
}

func (e *samplerCacheExt) OnSample(
	ctx context.Context,
	game chain.GameOnChain,
	reserves []*big.Int,
	totalPool *big.Int,
	yesPct, noPct float64,
	observedAt time.Time,
) {
	// 1. Auto-create a minimal gold_games row so that chain_states and
	//    price_history have something to join against. If the DApp later
	//    calls POST /games/sync the full metadata will overwrite this stub.
	e.ensureGameExists(ctx, game)

	// 2. Update chain state cache.
	state := &chainStateRow{
		GameID:          game.ID,
		ContractAddress: e.contractAddr,
		TotalPool:       game.TotalPool,
		IsResolved:      game.IsResolved,
		IsRefunded:      game.IsRefunded,
		WinningOption:   game.WinningOption,
		DeadlineSec:     game.DeadlineRaw,
		ReserveYes:      getReserve(reserves, 1),
		ReserveNo:       getReserve(reserves, 0),
		UpdatedAt:       observedAt.UTC().Format(time.RFC3339),
	}
	if err := e.chainStates.UpsertChainState(ctx, state); err != nil {
		slog.Warn("apiv1: sampler ext upsert chain state failed", "game_id", game.ID, "error", err)
	}

	// 3. Append price history point.
	point := &priceHistoryRow{
		GameID: game.ID,
		// The sampler already represents one observation interval. Store the
		// interval bucket rather than the request completion second so service
		// restarts/retries update the same chart point instead of duplicating it.
		TimestampSec: observedAt.Unix(),
		YesPrice:     yesPct,
		NoPrice:      noPct,
		TotalPool:    new(big.Int).Set(totalPool),
	}
	if err := e.history.AppendHistory(ctx, point); err != nil {
		slog.Warn("apiv1: sampler ext append history failed", "game_id", game.ID, "error", err)
	}
}

// ensureGameExists creates a stub row in gold_games when the game is seen for
// the first time. Uses INSERT IGNORE so it never overwrites metadata the DApp
// has already synced via POST /api/v1/gold/games/sync.
func (e *samplerCacheExt) ensureGameExists(ctx context.Context, game chain.GameOnChain) {
	if err := e.games.InsertGameStub(ctx, game.ID, e.contractAddr, game.IPFSCID); err != nil {
		slog.Warn("apiv1: sampler ext ensure game row failed", "game_id", game.ID, "error", err)
	}
}

// getReserve returns reserves[idx] if available, otherwise nil.
func getReserve(reserves []*big.Int, idx int) *big.Int {
	if idx < len(reserves) && reserves[idx] != nil {
		return new(big.Int).Set(reserves[idx])
	}
	return nil
}

// PopulateUserShares persists the signer's shares from getAllGamesExtraData
// to gold_user_positions. Called by the sampler after a successful batch
// cycle so the personal positions page can read from DB.
// This is NOT part of SamplerCacheExt — the sampler calls it via type assertion.
func (e *samplerCacheExt) PopulateUserShares(ctx context.Context, data *chain.AllGamesExtraData, games []chain.GameOnChain, signerAddr string) {
	if e.positions == nil || signerAddr == "" {
		return
	}

	upserted := 0
	for i, game := range games {
		if i >= len(data.MySharesYES) || i >= len(data.MySharesNO) {
			break
		}
		sharesYes := data.MySharesYES[i]
		sharesNo := data.MySharesNO[i]
		if sharesYes == nil {
			sharesYes = big.NewInt(0)
		}
		if sharesNo == nil {
			sharesNo = big.NewInt(0)
		}
		if sharesYes.Sign() == 0 && sharesNo.Sign() == 0 {
			continue
		}

		row := &userPositionRow{
			UserAddress: signerAddr,
			GameID:      game.ID,
			MySharesYes: sharesYes,
			MySharesNo:  sharesNo,
		}
		if err := e.positions.UpsertUserPosition(ctx, row); err != nil {
			slog.Warn("apiv1: sampler populate user shares failed", "game_id", game.ID, "error", err)
		} else {
			upserted++
		}
	}
	if upserted > 0 {
		slog.Info("apiv1: sampler populated user positions", "user", signerAddr, "games", upserted)
	}
}
