package aimanaged

import (
	"context"
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
	if point.Time != 120 || point.YesPercent != 25 || point.NoPercent != 75 {
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
		point.YesPercent != 25 || point.NoPercent != 75 {
		t.Fatalf("unexpected huge-reserve percentages: %+v", point)
	}
}

func TestObservationFromReservesCopiesRawChainValues(t *testing.T) {
	reserveNO := big.NewInt(25)
	reserveYES := big.NewInt(75)
	observation, err := observationFromReserves(&chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{reserveNO, reserveYES},
	}, time.Unix(120, 0))
	if err != nil {
		t.Fatal(err)
	}
	reserveNO.SetInt64(99)
	reserveYES.SetInt64(1)
	if observation.Source != historySourceChain || observation.ReserveNO.Cmp(big.NewInt(25)) != 0 ||
		observation.ReserveYES.Cmp(big.NewInt(75)) != 0 {
		t.Fatalf("raw reserves were not copied: %+v", observation)
	}
	if observation.YesPercent != 25 || observation.NoPercent != 75 {
		t.Fatalf("unexpected percentages: %+v", observation)
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

func TestMarketHistoryRepositoryMergesAndReturnsReserveCopies(t *testing.T) {
	store := newMarketHistoryStore(3, time.Minute)
	market := MarketIdentity{
		ContractAddress: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c",
		GameID:          1,
	}
	seed := []HistoryObservation{{
		Time: 100, YesPercent: 51, NoPercent: 49, Source: historySourceIPFS,
	}}
	current := HistoryObservation{
		Time: 121, YesPercent: 60, NoPercent: 40,
		ReserveNO: big.NewInt(40), ReserveYES: big.NewInt(60), Source: historySourceChain,
	}

	got, err := store.MergeAndList(context.Background(), market, seed, current, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Time != 120 || got[1].ReserveNO.Cmp(big.NewInt(40)) != 0 {
		t.Fatalf("unexpected repository history: %+v", got)
	}
	got[1].ReserveNO.SetInt64(99)
	snapshot, err := store.List(context.Background(), market, 3)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot[1].ReserveNO.Cmp(big.NewInt(40)) != 0 {
		t.Fatal("repository returned aliased reserve integers")
	}
}
