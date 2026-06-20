package chain

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Contract ABI JSON definition
const contractABI = `[{"constant":true,"inputs":[],"name":"getAllGames","outputs":[{"name":"ids","type":"uint256[]"},{"name":"cids","type":"string[]"},{"name":"pools","type":"uint256[]"},{"name":"deadlines","type":"uint256[]"},{"name":"resolved","type":"bool[]"},{"name":"refunded","type":"bool[]"},{"name":"winners","type":"uint8[]"}],"payable":false,"stateMutability":"view","type":"function"}]`

var parsedABI abi.ABI

func init() {
	var err error
	parsedABI, err = abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse contract ABI: %v", err))
	}
}

// DecodeGetAllGames uses go-ethereum's ABI library to properly decode the result
func DecodeGetAllGames(hexResult string) ([]GameOnChain, error) {
	// Strip 0x prefix
	data := fromHex(hexResult)

	// Unpack the result
	results, err := parsedABI.Unpack("getAllGames", data)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getAllGames: %w", err)
	}

	if len(results) < 7 {
		return nil, fmt.Errorf("unexpected number of results: %d", len(results))
	}

	// Type assertions
	ids, ok := results[0].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("ids is not []*big.Int")
	}

	cids, ok := results[1].([]string)
	if !ok {
		return nil, fmt.Errorf("cids is not []string")
	}

	pools, ok := results[2].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("pools is not []*big.Int")
	}

	deadlines, ok := results[3].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("deadlines is not []*big.Int")
	}

	resolved, ok := results[4].([]bool)
	if !ok {
		return nil, fmt.Errorf("resolved is not []bool")
	}

	refunded, ok := results[5].([]bool)
	if !ok {
		return nil, fmt.Errorf("refunded is not []bool")
	}

	winnersInterface, ok := results[6].([]uint8)
	var winners []uint8
	if !ok {
		// Try to decode as []interface{} first
		winnersRaw, ok := results[6].([]interface{})
		if !ok {
			return nil, fmt.Errorf("winners is not []uint8 or []interface{}")
		}
		winners = make([]uint8, len(winnersRaw))
		for i, v := range winnersRaw {
			winners[i] = v.(uint8)
		}
	} else {
		winners = winnersInterface
	}

	// Verify all arrays have the same length
	count := len(ids)
	if len(cids) != count || len(pools) != count || len(deadlines) != count || len(resolved) != count || len(refunded) != count || len(winners) != count {
		return nil, fmt.Errorf("array length mismatch")
	}

	// Build the games array
	games := make([]GameOnChain, count)
	for i := 0; i < count; i++ {
		games[i] = GameOnChain{
			ID:            int(ids[i].Int64()),
			IPFSCID:       cids[i],
			TotalPool:     pools[i],
			DeadlineRaw:   deadlines[i].Int64(),
			IsResolved:    resolved[i],
			IsRefunded:    refunded[i],
			WinningOption: int(winners[i]),
		}
	}

	return games, nil
}

func fromHex(hexStr string) []byte {
	hexStr = strings.TrimPrefix(strings.TrimSpace(hexStr), "0x")
	b, _ := hex.DecodeString(hexStr)
	return b
}
