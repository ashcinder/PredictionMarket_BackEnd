package aimanaged

import (
	"math"
	"math/big"
	"testing"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/oracle"
)

func TestComputePreAnalysisUsesOppositeReserveAMMConvention(t *testing.T) {
	pre := ComputePreAnalysis(
		&chain.GameExtraData{VirtualReservesNOYES: []*big.Int{big.NewInt(10), big.NewInt(90)}},
		&oracle.Quote{PriceUSD: 4000},
		time.Now().Add(time.Hour).Unix(),
		nil,
		time.Now(),
	)
	if math.Abs(pre.MarketProbYES-0.1) > 0.000001 {
		t.Fatalf("MarketProbYES should follow displayed yes_percent=reserveNO/total, got %.6f", pre.MarketProbYES)
	}
	if math.Abs(pre.MarketProbNO-0.9) > 0.000001 {
		t.Fatalf("MarketProbNO should follow displayed no_percent=reserveYES/total, got %.6f", pre.MarketProbNO)
	}
}
