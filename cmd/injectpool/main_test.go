package main

import (
	"math"
	"math/big"
	"testing"

	"PredictionMarket/internal/chain"
)

func TestParseBKCToWei(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "whole", raw: "1", want: "1000000000000000000"},
		{name: "fraction", raw: "1.25", want: "1250000000000000000"},
		{name: "leading decimal point", raw: ".5", want: "500000000000000000"},
		{name: "smallest unit", raw: "0.000000000000000001", want: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBKCToWei(tt.raw)
			if err != nil {
				t.Fatalf("parseBKCToWei(%q) error: %v", tt.raw, err)
			}
			if got.String() != tt.want {
				t.Fatalf("parseBKCToWei(%q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseBKCToWeiRejectsInvalidAmounts(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "1.0000000000000000001", "abc"} {
		if _, err := parseBKCToWei(raw); err == nil {
			t.Fatalf("parseBKCToWei(%q) succeeded, want error", raw)
		}
	}
}

func TestParseOptionPattern(t *testing.T) {
	got, err := parseOptionPattern("yes,no,0,1,Y,N")
	if err != nil {
		t.Fatalf("parseOptionPattern error: %v", err)
	}
	want := []int{0, 1, 0, 1, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pattern[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestShareDeltaForOption(t *testing.T) {
	before := &chain.GameExtraData{MySharesYESNO: []*big.Int{big.NewInt(10), big.NewInt(4)}}
	after := &chain.GameExtraData{MySharesYESNO: []*big.Int{big.NewInt(17), big.NewInt(4)}}

	got := shareDeltaForOption(before, after, 0)
	if got.String() != "7" {
		t.Fatalf("shareDeltaForOption YES = %s, want 7", got)
	}

	got = shareDeltaForOption(after, before, 0)
	if got.Sign() != 0 {
		t.Fatalf("negative share delta should clamp to zero, got %s", got)
	}
}

func TestPricesFromExtraUsesOppositeReserveForYes(t *testing.T) {
	extra := &chain.GameExtraData{
		VirtualReservesNOYES: []*big.Int{big.NewInt(75), big.NewInt(25)},
	}
	yes, no, err := pricesFromExtra(extra)
	if err != nil {
		t.Fatalf("pricesFromExtra error: %v", err)
	}
	if math.Abs(yes-75) > 0.000001 {
		t.Fatalf("yes price = %f, want 75", yes)
	}
	if math.Abs(no-25) > 0.000001 {
		t.Fatalf("no price = %f, want 25", no)
	}
}
