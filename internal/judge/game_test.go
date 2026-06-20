package judge

import (
	"testing"

	"PredictionMarket/internal/oracle"
)

func TestEvaluateWinnerPriceUp(t *testing.T) {
	quote := &oracle.Quote{Change24h: 1.5}
	got := EvaluateWinner("博弈黄金价格在 ... 相对基准 上涨 (Price Up)", quote)
	if got != 0 {
		t.Fatalf("expected YES(0), got %d", got)
	}
}

func TestEvaluateWinnerPriceDown(t *testing.T) {
	quote := &oracle.Quote{Change24h: -2.0}
	got := EvaluateWinner("博弈黄金价格在 ... 相对基准 下跌 (Price Down)", quote)
	if got != 0 {
		t.Fatalf("expected YES(0), got %d", got)
	}
}

func TestEvaluateWinnerBarrier(t *testing.T) {
	quote := &oracle.Quote{PriceUSD: 2600}
	got := EvaluateWinner("金价曾触及 2500 USD (...)", quote)
	if got != 0 {
		t.Fatalf("expected YES(0), got %d", got)
	}
}
