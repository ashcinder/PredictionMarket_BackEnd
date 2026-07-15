package chainlinkfeed

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestLiveEthereumFeeds(t *testing.T) {
	if os.Getenv("PREDICTIONMARKET_CHAINLINK_LIVE_TEST") != "1" {
		t.Skip("set PREDICTIONMARKET_CHAINLINK_LIVE_TEST=1 to query public Ethereum RPCs")
	}
	client := NewClient([]string{
		"https://1rpc.io/eth",
		"https://rpc.mevblocker.io",
		"https://eth-mainnet.public.blastapi.io",
		"https://rpc.eth.gateway.fm",
		"https://eth-pokt.nodies.app",
	}, 15*time.Second)
	feeds := map[string]common.Address{
		"XAU/USD": common.HexToAddress("0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6"),
		"BTC/USD": common.HexToAddress("0xF4030086522a5bEEa4988F8cA5B36dbC97BeE88c"),
	}
	for name, feed := range feeds {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			round, err := client.LatestRound(ctx, feed)
			if err != nil {
				t.Fatal(err)
			}
			if round.ID == nil || round.ID.Sign() <= 0 || round.Price <= 0 || round.UpdatedAt.IsZero() {
				t.Fatalf("invalid live round: %+v", round)
			}
			if round.UpdatedAt.After(time.Now().Add(time.Minute)) {
				t.Fatalf("source timestamp is in the future: %s", round.UpdatedAt)
			}
			t.Logf("feed=%s round=%s price=%.8f source_time=%s",
				feed.Hex(), round.ID.String(), round.Price, round.UpdatedAt.Format(time.RFC3339))
		})
	}
}
