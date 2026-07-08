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

func TestLoadParticipantKeysAllowsAutomaticRandomMode(t *testing.T) {
	keys, err := loadParticipantKeys("", "")
	if err != nil {
		t.Fatalf("loadParticipantKeys returned error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("loadParticipantKeys returned %d keys, want 0", len(keys))
	}
}

func TestBuildRandomParticipantsUsesReasonableRanges(t *testing.T) {
	minAmount, err := parseBKCToWei("0.2")
	if err != nil {
		t.Fatal(err)
	}
	maxAmount, err := parseBKCToWei("2")
	if err != nil {
		t.Fatal(err)
	}
	stepAmount, err := parseBKCToWei("0.01")
	if err != nil {
		t.Fatal(err)
	}

	participants, err := buildRandomParticipants(7, nil, minAmount, maxAmount)
	if err != nil {
		t.Fatalf("buildRandomParticipants error: %v", err)
	}
	if len(participants) != 7 {
		t.Fatalf("participant count = %d, want 7", len(participants))
	}

	seenAddresses := map[string]struct{}{}
	yesCount := 0
	noCount := 0
	for _, p := range participants {
		if p.privateKey == "" {
			t.Fatal("generated participant has empty private key")
		}
		if p.address == "" {
			t.Fatal("generated participant has empty address")
		}
		if _, ok := seenAddresses[p.address]; ok {
			t.Fatalf("duplicate generated address %s", p.address)
		}
		seenAddresses[p.address] = struct{}{}
		if p.amountWei.Cmp(minAmount) < 0 || p.amountWei.Cmp(maxAmount) > 0 {
			t.Fatalf("amount %s outside range [%s,%s]", p.amountWei, minAmount, maxAmount)
		}
		if new(big.Int).Mod(p.amountWei, stepAmount).Sign() != 0 {
			t.Fatalf("amount %s is not aligned to 0.01 BKC", p.amountWei)
		}
		switch p.optionID {
		case 0:
			yesCount++
		case 1:
			noCount++
		default:
			t.Fatalf("optionID = %d, want 0 or 1", p.optionID)
		}
	}
	if yesCount < 3 || noCount < 3 {
		t.Fatalf("options are not balanced enough: yes=%d no=%d", yesCount, noCount)
	}
}

func TestGeneratedParticipantFundingCoversStakeAndBuyGas(t *testing.T) {
	stake := big.NewInt(1_000_000_000_000_000_000)
	gasPrice := big.NewInt(2)
	got := generatedParticipantFunding(stake, gasPrice)

	want := new(big.Int).Set(stake)
	want.Add(want, new(big.Int).Mul(gasPrice, big.NewInt(generatedBuyGasLimit)))
	want.Add(want, generatedFundingBufferWei())
	if got.Cmp(want) != 0 {
		t.Fatalf("generatedParticipantFunding = %s, want %s", got, want)
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
