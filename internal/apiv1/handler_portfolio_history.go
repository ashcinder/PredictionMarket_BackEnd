package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const portfolioSnapshotBucketSec int64 = 5 * 60

func (s *Server) handleGetPortfolioHistory(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "GET,OPTIONS") {
		return
	}
	logRequest(r)
	if s.portfolioHistory == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "portfolio history unavailable")
		return
	}
	userAddress := strings.TrimSpace(r.URL.Query().Get("user_address"))
	if !common.IsHexAddress(userAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid user_address")
		return
	}
	limit := 256
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 2 || parsed > 1000 {
			writeJSONError(w, http.StatusBadRequest, "limit must be between 2 and 1000")
			return
		}
		limit = parsed
	}
	points, err := s.portfolioHistory.ListPortfolioHistory(r.Context(), userAddress, limit)
	if err != nil {
		slog.Warn("apiv1: list portfolio history failed", "user", userAddress, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to fetch portfolio history")
		return
	}
	if points == nil {
		points = []PortfolioHistoryPointDTO{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"history": points})
}

func (s *Server) handleAddPortfolioHistory(w http.ResponseWriter, r *http.Request) {
	if setCORS(w, r, "POST,OPTIONS") {
		return
	}
	logRequest(r)
	if s.portfolioHistory == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "portfolio history unavailable")
		return
	}
	var req AddPortfolioHistoryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !common.IsHexAddress(req.UserAddress) {
		writeJSONError(w, http.StatusBadRequest, "invalid user_address")
		return
	}
	value := parseBigIntStr(req.TotalValueWei)
	if value == nil || value.Sign() < 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid total_value_wei")
		return
	}
	if req.ActiveMarketCount < 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid active_market_count")
		return
	}
	now := time.Now().Unix()
	timestamp := now - now%portfolioSnapshotBucketSec
	point := &portfolioHistoryRow{
		UserAddress: req.UserAddress, TimestampSec: timestamp,
		TotalValueWei: value, ActiveMarketCount: req.ActiveMarketCount,
	}
	if err := s.portfolioHistory.UpsertPortfolioHistory(r.Context(), point); err != nil {
		slog.Warn("apiv1: upsert portfolio history failed", "user", req.UserAddress, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "failed to save portfolio history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "timestamp_sec": timestamp})
}
