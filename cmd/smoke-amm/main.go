// Command smoke-amm verifies a deployed local AMM contract with a complete
// create -> buy -> quote -> sell round trip. It intentionally creates a market,
// so use it only against a disposable local deployment.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

const createABI = `[{"inputs":[{"name":"_ipfsCID","type":"string"},{"name":"_durationSec","type":"uint256"}],"name":"createGame","outputs":[],"stateMutability":"payable","type":"function"}]`

func main() {
	configPath := flag.String("config", "config.yaml", "backend YAML configuration")
	flag.Parse()
	cfg, err := config.LoadFile(*configPath)
	must(err)
	client, err := chain.NewClient(
		cfg.PrivateKey, cfg.ContractAddress, cfg.RPCURL, cfg.BrokerChainURL, cfg.UseBrokerChain)
	must(err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	create, err := abi.JSON(strings.NewReader(createABI))
	must(err)
	createData, err := create.Pack("createGame", "local-v1:cn-amm-smoke", big.NewInt(3600))
	must(err)
	initial := tokens(100)
	_, err = client.SendTransaction(ctx, "0x"+hex.EncodeToString(createData), initial)
	must(err)

	buyData, err := chain.EncodeBuyShares(1, 0)
	must(err)
	_, err = client.SendTransaction(ctx, buyData, tokens(10))
	must(err)

	extraData, err := chain.EncodeGetGameExtraData(1, client.WalletAddress())
	must(err)
	extraHex, err := client.EthCall(ctx, extraData)
	must(err)
	extra, err := chain.DecodeGetGameExtraData(extraHex)
	must(err)
	shares := extra.MySharesYESNO[0]
	if shares.Sign() <= 0 {
		must(fmt.Errorf("buy produced no YES shares"))
	}
	quoteData, err := chain.EncodeQuoteSellShares(1, 0, shares)
	must(err)
	quoteHex, err := client.EthCall(ctx, quoteData)
	must(err)
	amountOut, err := chain.DecodeQuoteSellShares(quoteHex)
	must(err)
	sellData, err := chain.EncodeSellShares(1, 0, shares, amountOut)
	must(err)
	_, err = client.SendTransaction(ctx, sellData, big.NewInt(0))
	must(err)

	finalExtraHex, err := client.EthCall(ctx, extraData)
	must(err)
	finalExtra, err := chain.DecodeGetGameExtraData(finalExtraHex)
	must(err)
	if finalExtra.MySharesYESNO[0].Sign() != 0 {
		must(fmt.Errorf("YES shares remain after full sale: %s", finalExtra.MySharesYESNO[0]))
	}
	infoData, err := chain.EncodeGetGameInfo(1)
	must(err)
	infoHex, err := client.EthCall(ctx, infoData)
	must(err)
	info, err := chain.DecodeGetGameInfo(1, infoHex)
	must(err)
	if info.TotalPool.Cmp(initial) != 0 {
		must(fmt.Errorf("round-trip pool mismatch: got %s want %s", info.TotalPool, initial))
	}
	fmt.Printf("smoke_ok game_id=1 shares_sold=%s amount_out=%s final_pool=%s\n",
		shares, amountOut, info.TotalPool)
}

func tokens(value int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(value), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "smoke-amm:", err)
		os.Exit(1)
	}
}
