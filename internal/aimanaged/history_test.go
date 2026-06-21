package aimanaged

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/ipfs"
)

func TestPointFromReservesUsesContractNOYESOrder(t *testing.T) {
	point, err := pointFromReserves(&chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{big.NewInt(25), big.NewInt(75)},
	}, time.Unix(120, 0))
	if err != nil {
		t.Fatal(err)
	}
	if point.Time != 120 || point.YesPercent != 75 || point.NoPercent != 25 {
		t.Fatalf("unexpected point: %+v", point)
	}
}

func TestPointFromReservesSupportsHugeIntegers(t *testing.T) {
	no := new(big.Int).Exp(big.NewInt(10), big.NewInt(200), nil)
	yes := new(big.Int).Mul(new(big.Int).Set(no), big.NewInt(3))

	point, err := pointFromReserves(&chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{no, yes},
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if math.IsNaN(point.YesPercent) || math.IsInf(point.YesPercent, 0) ||
		point.YesPercent != 75 || point.NoPercent != 25 {
		t.Fatalf("unexpected huge-reserve percentages: %+v", point)
	}
}

func TestPointFromReservesRejectsInvalidReserves(t *testing.T) {
	tests := map[string]*chain.GameExtraData{
		"nil extra":    nil,
		"missing pair": {VirtualReservesNOYES: []*big.Int{big.NewInt(1)}},
		"nil reserve":  {VirtualReservesNOYES: []*big.Int{nil, big.NewInt(1)}},
		"negative":     {VirtualReservesNOYES: []*big.Int{big.NewInt(-1), big.NewInt(2)}},
		"zero total":   {VirtualReservesNOYES: []*big.Int{big.NewInt(0), big.NewInt(0)}},
	}
	for name, extra := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := pointFromReserves(extra, time.Unix(1, 0)); err == nil {
				t.Fatal("expected invalid reserves error")
			}
		})
	}
}

func TestMarketKeyNormalizesContractAndSeparatesGames(t *testing.T) {
	contract := "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
	if marketKey(contract, 7) != marketKey(strings.ToLower(contract), 7) {
		t.Fatal("contract address case changed the market key")
	}
	if marketKey(contract, 7) == marketKey(contract, 8) {
		t.Fatal("different game IDs shared a market key")
	}
}

func TestMarketHistoryMergesSortsBucketsAndDeduplicates(t *testing.T) {
	store := newMarketHistoryStore(10, time.Minute)
	key := marketKey("0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c", 7)
	seed := []ipfs.HistoryPoint{
		{Time: 300, YesPercent: 53, NoPercent: 47},
		{Time: 100, YesPercent: 51, NoPercent: 49},
		{Time: 200, YesPercent: 52, NoPercent: 48},
		{Time: 200, YesPercent: 62, NoPercent: 38},
	}

	got := store.MergeAndAppend(key, seed, ipfs.HistoryPoint{Time: 370, YesPercent: 60, NoPercent: 40})
	wantTimes := []int64{100, 200, 300, 360}
	if len(got) != len(wantTimes) {
		t.Fatalf("unexpected history length: %+v", got)
	}
	for i, want := range wantTimes {
		if got[i].Time != want {
			t.Fatalf("point %d time=%d, want %d: %+v", i, got[i].Time, want, got)
		}
	}
	if got[1].YesPercent != 62 {
		t.Fatalf("duplicate seed did not keep last value: %+v", got[1])
	}

	got = store.MergeAndAppend(key, nil, ipfs.HistoryPoint{Time: 419, YesPercent: 65, NoPercent: 35})
	if len(got) != 4 || got[3].Time != 360 || got[3].YesPercent != 65 {
		t.Fatalf("same poll bucket was not replaced: %+v", got)
	}
}

func TestMarketHistoryCapsNewestPointsAndReturnsCopies(t *testing.T) {
	store := newMarketHistoryStore(3, time.Second)
	key := marketKey("0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c", 1)
	seed := []ipfs.HistoryPoint{
		{Time: 1, YesPercent: 51, NoPercent: 49},
		{Time: 2, YesPercent: 52, NoPercent: 48},
		{Time: 3, YesPercent: 53, NoPercent: 47},
	}
	got := store.MergeAndAppend(key, seed, ipfs.HistoryPoint{Time: 4, YesPercent: 54, NoPercent: 46})
	if len(got) != 3 || got[0].Time != 2 || got[2].Time != 4 {
		t.Fatalf("history did not retain newest points: %+v", got)
	}

	got[0].YesPercent = 99
	snapshot := store.Snapshot(key)
	if snapshot[0].YesPercent == 99 {
		t.Fatal("returned history aliases internal storage")
	}
}
