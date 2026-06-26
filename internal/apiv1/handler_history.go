package apiv1

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"PredictionMarket/internal/chain"
)

// handleGetHistory handles GET /api/v1/gold/games/{id}/history
func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)

	gameID, ok := parsePositiveIntFromPath(r, "id")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id")
		return
	}

	limit := s.historyMax
	if limit < 1 {
		limit = 256
	}

	points, err := s.history.ListHistory(r.Context(), gameID, limit)
	if err != nil {
		slog.Warn("apiv1: list history failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to fetch history")
		return
	}

	// When the database has no history yet, do NOT block the HTTP response
	// waiting for a slow chain call. Instead, fire a background goroutine to
	// populate the cache and return an empty array immediately. The frontend
	// gets a fast response and the next request will have the cached data.
	//
	// The background goroutine uses a detached context so it survives client
	// disconnection/timeout.
	if len(points) == 0 && s.chain != nil {
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.sampleOnDemand(bgCtx, gameID)
		}()
	}

	if points == nil {
		points = []PricePointDTO{}
	}

	slog.Info("apiv1: get history response", "game_id", gameID, "points", len(points))
	writeJSON(w, http.StatusOK, map[string]interface{}{"history": points})
}

// handleAddHistory handles POST /api/v1/gold/games/{id}/history
func (s *Server) handleAddHistory(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "POST,OPTIONS") {
		return
	}
	logRequest(r)

	gameID, ok := parsePositiveIntFromPath(r, "id")
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid game_id")
		return
	}

	var req AddHistoryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Use the path game_id if the body doesn't provide one (or overrides with
	// the body value if present).
	effectiveGameID := gameID
	if req.GameID > 0 {
		effectiveGameID = req.GameID
	}

	row := &priceHistoryRow{
		GameID:       effectiveGameID,
		TimestampSec: req.TimestampSec,
		YesPrice:     req.YesPrice,
		NoPrice:      req.NoPrice,
		TotalPool:    parseBigIntStr(req.TotalPool),
	}
	if err := s.history.AppendHistory(r.Context(), row); err != nil {
		slog.Warn("apiv1: append history failed", "game_id", effectiveGameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "failed to add history point")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]bool{"success": true})
}

// sampleOnDemand fetches current reserves from chain, computes YES/NO
// percentages, persists them, and returns the data point. Returns nil on any
// error — the caller can return an empty array (the DApp falls back to chain
// reads directly).
func (s *Server) sampleOnDemand(ctx context.Context, gameID int) *PricePointDTO {
	encoded, err := chain.EncodeGetGameExtraData(gameID, s.chain.WalletAddress())
	if err != nil {
		slog.Warn("apiv1: on-demand encode failed", "game_id", gameID, "error", err)
		return nil
	}
	hexResult, err := s.chain.EthCall(ctx, encoded)
	if err != nil {
		slog.Warn("apiv1: on-demand eth_call failed", "game_id", gameID, "error", err)
		return nil
	}
	extra, err := chain.DecodeGetGameExtraData(hexResult)
	if err != nil {
		slog.Warn("apiv1: on-demand decode failed", "game_id", gameID, "error", err)
		return nil
	}

	now := time.Now()
	yesPct, noPct, totalPool := computePrices(extra.VirtualReservesNOYES)

	row := &priceHistoryRow{
		GameID:       gameID,
		TimestampSec: now.Unix(),
		YesPrice:     yesPct,
		NoPrice:      noPct,
		TotalPool:    totalPool,
	}
	if err := s.history.AppendHistory(ctx, row); err != nil {
		slog.Warn("apiv1: on-demand persist failed", "game_id", gameID, "error", err)
		return nil
	}

	return &PricePointDTO{
		GameID:       gameID,
		TimestampSec: now.Unix(),
		YesPrice:     yesPct,
		NoPrice:      noPct,
		TotalPool:    bigIntOrZero(totalPool),
	}
}

// computePrices calculates YES/NO percentages from virtual reserves.
// reserves[0] = reserveNO, reserves[1] = reserveYES.
func computePrices(reserves []*big.Int) (yesPct, noPct float64, totalPool *big.Int) {
	var rNO, rYES *big.Int
	if len(reserves) > 0 && reserves[0] != nil {
		rNO = new(big.Int).Set(reserves[0])
	} else {
		rNO = big.NewInt(0)
	}
	if len(reserves) > 1 && reserves[1] != nil {
		rYES = new(big.Int).Set(reserves[1])
	} else {
		rYES = big.NewInt(0)
	}

	total := new(big.Int).Add(rNO, rYES)
	if total.Sign() <= 0 {
		return 50, 50, big.NewInt(0)
	}

	// YES% = reserveYES / (reserveNO + reserveYES) * 100
	// NO%  = reserveNO  / (reserveNO + reserveYES) * 100
	yesRat := new(big.Rat).SetFrac(rYES, total)
	noRat := new(big.Rat).SetFrac(rNO, total)
	hundred := new(big.Rat).SetInt64(100)
	yesRat.Mul(yesRat, hundred)
	noRat.Mul(noRat, hundred)
	yes, _ := yesRat.Float64()
	no, _ := noRat.Float64()
	return yes, no, total
}

func (s *Server) historyFromIPFS(ctx context.Context, gameID int) []PricePointDTO {
	if s.metadata == nil || s.games == nil {
		return nil
	}

	game, err := s.games.GetGameByID(ctx, gameID)
	if err != nil {
		slog.Warn("apiv1: get game for ipfs history failed", "game_id", gameID, "error", err)
		return nil
	}
	if game == nil || game.IPFSCID == "" {
		return nil
	}

	meta, err := s.metadata.DownloadMetadata(game.IPFSCID)
	if err != nil {
		slog.Warn("apiv1: ipfs history hydration failed", "game_id", gameID, "cid", game.IPFSCID, "error", err)
		return nil
	}
	if meta == nil || len(meta.History) == 0 {
		return nil
	}

	points := make([]PricePointDTO, 0, len(meta.History))
	for _, point := range meta.History {
		points = append(points, PricePointDTO{
			GameID:       gameID,
			TimestampSec: point.Time,
			YesPrice:     point.YesPercent,
			NoPrice:      point.NoPercent,
			TotalPool:    "0",
		})
	}
	return points
}
