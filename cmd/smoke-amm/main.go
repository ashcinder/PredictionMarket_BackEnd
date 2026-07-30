// Command smoke-amm verifies a deployed local AMM contract with a complete
// create -> buy -> sell -> add liquidity -> remove liquidity round trip. It
// intentionally creates a market, so use it only against a disposable local
// deployment.
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
	"github.com/ethereum/go-ethereum/common"
)

const liquidityABI = `[
  {"inputs":[{"name":"_ipfsCID","type":"string"},{"name":"_durationSec","type":"uint256"}],"name":"createGame","outputs":[],"stateMutability":"payable","type":"function"},
  {"inputs":[{"name":"_gameId","type":"uint256"},{"name":"_minLiquidityShares","type":"uint256"}],"name":"addLiquidity","outputs":[],"stateMutability":"payable","type":"function"},
  {"inputs":[{"name":"_gameId","type":"uint256"},{"name":"_provider","type":"address"},{"name":"_liquidityShareAmount","type":"uint256"}],"name":"quoteRemoveLiquidity","outputs":[{"name":"collateralOut","type":"uint256"},{"name":"feeOut","type":"uint256"},{"name":"returnedYES","type":"uint256"},{"name":"returnedNO","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"name":"_gameId","type":"uint256"},{"name":"_liquidityShareAmount","type":"uint256"},{"name":"_minAmountOut","type":"uint256"}],"name":"removeLiquidity","outputs":[],"stateMutability":"nonpayable","type":"function"},
  {"inputs":[{"name":"_gameId","type":"uint256"},{"name":"_provider","type":"address"}],"name":"getLiquidityPosition","outputs":[{"name":"_totalLiquidityShares","type":"uint256"},{"name":"_myLiquidityShares","type":"uint256"},{"name":"_claimableFees","type":"uint256"},{"name":"_feePool","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

func main() {
	configPath := flag.String("config", "config.yaml", "backend YAML configuration")
	contractAddress := flag.String("contract", "", "optional deployed contract address override")
	flag.Parse()
	cfg, err := config.LoadFile(*configPath)
	must(err)
	if strings.TrimSpace(*contractAddress) != "" {
		cfg.ContractAddress = strings.TrimSpace(*contractAddress)
	}
	client, err := chain.NewClient(
		cfg.PrivateKey, cfg.ContractAddress, cfg.RPCURL, cfg.BrokerChainURL, cfg.UseBrokerChain)
	must(err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	contractABI, err := abi.JSON(strings.NewReader(liquidityABI))
	must(err)
	createData, err := contractABI.Pack(
		"createGame", "local-v1:cn-amm-liquidity-smoke", big.NewInt(3600))
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
	if info.TotalPool.Cmp(initial) <= 0 {
		must(fmt.Errorf("trading fees were not retained by the pool: got %s initial %s",
			info.TotalPool, initial))
	}

	positionBefore, err := liquidityPosition(ctx, client, contractABI)
	must(err)
	if positionBefore[0].Cmp(initial) != 0 || positionBefore[1].Cmp(initial) != 0 {
		must(fmt.Errorf("creator LP mismatch: total=%s mine=%s want=%s",
			positionBefore[0], positionBefore[1], initial))
	}
	if positionBefore[2].Sign() <= 0 {
		must(fmt.Errorf("creator did not accrue trading fees"))
	}

	addAmount := tokens(20)
	addData, err := contractABI.Pack("addLiquidity", big.NewInt(1), big.NewInt(1))
	must(err)
	_, err = client.SendTransaction(
		ctx, "0x"+hex.EncodeToString(addData), addAmount)
	must(err)
	positionAfterAdd, err := liquidityPosition(ctx, client, contractABI)
	must(err)
	minted := new(big.Int).Sub(positionAfterAdd[1], positionBefore[1])
	if minted.Sign() <= 0 {
		must(fmt.Errorf("liquidity contribution minted no LP shares"))
	}

	lockedRemoveData, err := contractABI.Pack(
		"removeLiquidity", big.NewInt(1), initial, big.NewInt(0))
	must(err)
	if _, err := client.SendTransaction(
		ctx, "0x"+hex.EncodeToString(lockedRemoveData), big.NewInt(0)); err == nil {
		must(fmt.Errorf("creator initial liquidity was unexpectedly withdrawable"))
	}

	quoteRemoveData, err := contractABI.Pack(
		"quoteRemoveLiquidity", big.NewInt(1),
		common.HexToAddress(client.WalletAddress()), minted)
	must(err)
	quoteRemoveHex, err := client.EthCall(
		ctx, "0x"+hex.EncodeToString(quoteRemoveData))
	must(err)
	quoteRemove, err := unpackBigInts(contractABI, "quoteRemoveLiquidity", quoteRemoveHex)
	must(err)
	minAmountOut := new(big.Int).Add(quoteRemove[0], quoteRemove[1])
	removeData, err := contractABI.Pack(
		"removeLiquidity", big.NewInt(1), minted, minAmountOut)
	must(err)
	_, err = client.SendTransaction(
		ctx, "0x"+hex.EncodeToString(removeData), big.NewInt(0))
	must(err)

	positionAfterRemove, err := liquidityPosition(ctx, client, contractABI)
	must(err)
	if positionAfterRemove[1].Cmp(initial) != 0 {
		must(fmt.Errorf("LP removal mismatch: mine=%s want=%s",
			positionAfterRemove[1], initial))
	}
	fmt.Printf(
		"smoke_ok game_id=1 shares_sold=%s sell_amount=%s creator_fee=%s lp_minted=%s lp_removed=%s\n",
		shares, amountOut, positionBefore[2], minted, minAmountOut)
}

func liquidityPosition(
	ctx context.Context, client *chain.Client, contractABI abi.ABI,
) ([]*big.Int, error) {
	data, err := contractABI.Pack(
		"getLiquidityPosition", big.NewInt(1),
		common.HexToAddress(client.WalletAddress()))
	if err != nil {
		return nil, err
	}
	raw, err := client.EthCall(ctx, "0x"+hex.EncodeToString(data))
	if err != nil {
		return nil, err
	}
	return unpackBigInts(contractABI, "getLiquidityPosition", raw)
}

func unpackBigInts(contractABI abi.ABI, method, raw string) ([]*big.Int, error) {
	encoded, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	if err != nil {
		return nil, err
	}
	values, err := contractABI.Unpack(method, encoded)
	if err != nil {
		return nil, err
	}
	result := make([]*big.Int, 0, len(values))
	for _, value := range values {
		number, ok := value.(*big.Int)
		if !ok {
			return nil, fmt.Errorf("%s returned non-integer value %T", method, value)
		}
		result = append(result, number)
	}
	return result, nil
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
