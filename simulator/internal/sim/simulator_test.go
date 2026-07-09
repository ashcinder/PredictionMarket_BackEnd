package sim

import (
	"math/big"
	"testing"

	dbwriter "predictionmarket-simulator/internal/db"
)

func TestExecuteOffchainTradeMatchesContractBuyYesFormula(t *testing.T) {
	initial := big.NewInt(100)
	state := newOffchainMarketState(7, "sim-cid", initial, 12345)

	trade, err := executeOffchainTrade(state, "0x1234567890123456789012345678901234567890", 0, big.NewInt(25))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := state.reserveNo.String(), "125"; got != want {
		t.Fatalf("reserveNo = %s, want %s", got, want)
	}
	if got, want := state.reserveYes.String(), "80"; got != want {
		t.Fatalf("reserveYes = %s, want %s", got, want)
	}
	if got, want := trade.ShareAmountWei.String(), "45"; got != want {
		t.Fatalf("share amount = %s, want %s", got, want)
	}
	if got, want := dbwriter.ShareAt(trade.Extra.VirtualReservesNOYES, 0).String(), "125"; got != want {
		t.Fatalf("extra reserve NO = %s, want %s", got, want)
	}
	if got, want := dbwriter.ShareAt(trade.Extra.VirtualReservesNOYES, 1).String(), "80"; got != want {
		t.Fatalf("extra reserve YES = %s, want %s", got, want)
	}
	if got, want := trade.MySharesYesAfter, "45"; got != want {
		t.Fatalf("YES shares after = %s, want %s", got, want)
	}
}
