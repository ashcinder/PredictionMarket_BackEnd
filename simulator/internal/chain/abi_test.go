package chain

import (
	"strings"
	"testing"
)

func TestEncodeCreateGameUsesCurrentContractSignature(t *testing.T) {
	got, err := EncodeCreateGame("sim-cid", 3600)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "0x") {
		t.Fatalf("encoded data = %q, want hex", got)
	}
	// createGame(string,uint256)
	if got[:10] != "0x6dd5e67c" {
		t.Fatalf("selector = %s, want createGame(string,uint256)", got[:10])
	}
}

func TestEncodeBuySharesUsesCurrentContractSignature(t *testing.T) {
	got, err := EncodeBuyShares(7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[:10] != "0xcfd0d89b" {
		t.Fatalf("selector = %s, want buyShares(uint256,uint8)", got[:10])
	}
}

func TestEncodeBuySharesRejectsInvalidOption(t *testing.T) {
	if _, err := EncodeBuyShares(7, 3); err == nil {
		t.Fatal("EncodeBuyShares succeeded, want invalid option error")
	}
}
