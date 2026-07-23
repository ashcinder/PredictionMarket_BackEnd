package apiv1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
)

const maxChartHistoryPoints = 12000

type chartRange struct {
	name       string
	lookback   int64
	bucketSize int64
	queryLimit int
	targetSize int
}

func parseChartRange(raw string, fallbackLimit int) chartRange {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "30m":
		return chartRange{name: "30m", lookback: 30 * 60, bucketSize: 30, queryLimit: 96, targetSize: 90}
	case "1h":
		return chartRange{name: "1h", lookback: 60 * 60, bucketSize: 60, queryLimit: 160, targetSize: 120}
	case "1d":
		return chartRange{name: "1d", lookback: 24 * 60 * 60, bucketSize: 20 * 60, queryLimit: 1800, targetSize: 180}
	case "1w":
		return chartRange{name: "1w", lookback: 7 * 24 * 60 * 60, bucketSize: 2 * 60 * 60, queryLimit: maxChartHistoryPoints, targetSize: 220}
	case "all":
		return chartRange{name: "all", queryLimit: maxChartHistoryPoints, targetSize: 240}
	default:
		if fallbackLimit < 1 {
			fallbackLimit = 256
		}
		return chartRange{name: "all", queryLimit: fallbackLimit, targetSize: 240}
	}
}

func prepareChartHistory(points []PricePointDTO, window chartRange) []PricePointDTO {
	if len(points) == 0 {
		return []PricePointDTO{}
	}
	sort.SliceStable(points, func(i, j int) bool {
		return points[i].TimestampSec < points[j].TimestampSec
	})

	if window.lookback > 0 {
		cutoff := points[len(points)-1].TimestampSec - window.lookback
		first := sort.Search(len(points), func(i int) bool {
			return points[i].TimestampSec >= cutoff
		})
		if first > 0 {
			// Keep the preceding point so the chart starts with the state at the range boundary.
			first--
		}
		points = points[first:]
	}

	if window.targetSize < 3 || len(points) <= window.targetSize {
		return points
	}
	return largestTriangleThreeBuckets(points, window.targetSize)
}

// largestTriangleThreeBuckets keeps the first and last observations while
// selecting the visually most significant point from each bucket. Unlike
// simple "last point per bucket" sampling, it preserves short-lived market
// moves instead of flattening them out on day/week views.
func largestTriangleThreeBuckets(points []PricePointDTO, threshold int) []PricePointDTO {
	if threshold >= len(points) || threshold < 3 {
		return points
	}

	sampled := make([]PricePointDTO, 0, threshold)
	sampled = append(sampled, points[0])
	every := float64(len(points)-2) / float64(threshold-2)
	selectedIndex := 0

	for bucket := 0; bucket < threshold-2; bucket++ {
		avgStart := int(float64(bucket+1)*every) + 1
		avgEnd := int(float64(bucket+2)*every) + 1
		if avgEnd > len(points) {
			avgEnd = len(points)
		}
		if avgStart >= avgEnd {
			avgStart = minInt(avgStart, len(points)-1)
			avgEnd = minInt(avgStart+1, len(points))
		}

		var avgX, avgY float64
		for i := avgStart; i < avgEnd; i++ {
			avgX += float64(points[i].TimestampSec)
			avgY += points[i].YesPrice
		}
		avgCount := float64(avgEnd - avgStart)
		avgX /= avgCount
		avgY /= avgCount

		rangeStart := int(float64(bucket)*every) + 1
		rangeEnd := int(float64(bucket+1)*every) + 1
		if rangeEnd > len(points)-1 {
			rangeEnd = len(points) - 1
		}
		if rangeStart >= rangeEnd {
			rangeStart = minInt(rangeStart, len(points)-2)
			rangeEnd = rangeStart + 1
		}

		anchor := points[selectedIndex]
		maxArea := -1.0
		nextIndex := rangeStart
		for i := rangeStart; i < rangeEnd; i++ {
			area := absFloat64(
				(float64(anchor.TimestampSec)-avgX)*(points[i].YesPrice-anchor.YesPrice) -
					(float64(anchor.TimestampSec)-float64(points[i].TimestampSec))*(avgY-anchor.YesPrice),
			)
			if area > maxArea {
				maxArea = area
				nextIndex = i
			}
		}
		sampled = append(sampled, points[nextIndex])
		selectedIndex = nextIndex
	}
	sampled = append(sampled, points[len(points)-1])
	return sampled
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absFloat64(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

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

	window := parseChartRange(r.URL.Query().Get("range"), s.historyMax)

	points, err := s.history.ListHistory(r.Context(), gameID, window.queryLimit)
	if err != nil {
		slog.Warn("apiv1: list history failed", "game_id", gameID, "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "unable to fetch history")
		return
	}

	points = prepareChartHistory(points, window)

	slog.Info("apiv1: get history response", "game_id", gameID, "range", window.name, "points", len(points))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"history":    points,
		"range":      window.name,
		"bucket_sec": window.bucketSize,
	})
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
