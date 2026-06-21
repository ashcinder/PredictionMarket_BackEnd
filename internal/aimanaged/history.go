package aimanaged

import (
	"errors"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/ipfs"

	"github.com/ethereum/go-ethereum/common"
)

type marketHistoryStore struct {
	mu       sync.RWMutex
	max      int
	interval time.Duration
	points   map[string][]ipfs.HistoryPoint
}

func newMarketHistoryStore(max int, interval time.Duration) *marketHistoryStore {
	return &marketHistoryStore{
		max:      max,
		interval: interval,
		points:   make(map[string][]ipfs.HistoryPoint),
	}
}

func marketKey(contract string, gameID int) string {
	normalized := strings.ToLower(common.HexToAddress(contract).Hex())
	return normalized + ":" + strconv.Itoa(gameID)
}

func pointFromReserves(extra *chain.GameExtraData, observedAt time.Time) (ipfs.HistoryPoint, error) {
	if extra == nil || len(extra.VirtualReservesNOYES) < 2 ||
		extra.VirtualReservesNOYES[0] == nil || extra.VirtualReservesNOYES[1] == nil {
		return ipfs.HistoryPoint{}, errors.New("virtual reserves must contain NO and YES values")
	}

	reserveNO := extra.VirtualReservesNOYES[0]
	reserveYES := extra.VirtualReservesNOYES[1]
	if reserveNO.Sign() < 0 || reserveYES.Sign() < 0 {
		return ipfs.HistoryPoint{}, errors.New("virtual reserves must not be negative")
	}
	total := new(big.Int).Add(new(big.Int).Set(reserveNO), reserveYES)
	if total.Sign() == 0 {
		return ipfs.HistoryPoint{}, errors.New("virtual reserves total must be positive")
	}

	yesRatio := new(big.Rat).SetFrac(reserveYES, total)
	yesPercent, _ := yesRatio.Float64()
	yesPercent *= 100
	noPercent := 100 - yesPercent
	if math.IsNaN(yesPercent) || math.IsInf(yesPercent, 0) ||
		math.IsNaN(noPercent) || math.IsInf(noPercent, 0) {
		return ipfs.HistoryPoint{}, errors.New("virtual reserve percentages are not finite")
	}

	return ipfs.HistoryPoint{
		Time:       observedAt.Unix(),
		YesPercent: yesPercent,
		NoPercent:  noPercent,
	}, nil
}

func (s *marketHistoryStore) MergeAndAppend(key string, seed []ipfs.HistoryPoint, current ipfs.HistoryPoint) []ipfs.HistoryPoint {
	current.Time = bucketTimestamp(current.Time, s.interval)

	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.points[key]
	byTime := make(map[int64]ipfs.HistoryPoint, len(seed)+len(existing)+1)
	for _, point := range seed {
		byTime[point.Time] = point
	}
	for _, point := range existing {
		byTime[point.Time] = point
	}
	byTime[current.Time] = current

	points := make([]ipfs.HistoryPoint, 0, len(byTime))
	for _, point := range byTime {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Time < points[j].Time
	})
	if len(points) > s.max {
		points = append([]ipfs.HistoryPoint(nil), points[len(points)-s.max:]...)
	}
	s.points[key] = points
	return append([]ipfs.HistoryPoint(nil), points...)
}

func bucketTimestamp(timestamp int64, interval time.Duration) int64 {
	if interval <= 0 {
		return timestamp
	}
	return time.Unix(timestamp, 0).Truncate(interval).Unix()
}

func (s *marketHistoryStore) Snapshot(key string) []ipfs.HistoryPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ipfs.HistoryPoint(nil), s.points[key]...)
}
