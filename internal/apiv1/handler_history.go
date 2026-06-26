package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// handleGetHistory handles GET /api/v1/gold/games/{id}/history
// Pure DB read — no chain calls. History data is populated by the
// background sampler (MarketHistorySampler) which runs every minute.
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
