package chain

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Contract ABI JSON definition
const contractABI = `[
{"constant":true,"inputs":[],"name":"getAllGames","outputs":[{"name":"ids","type":"uint256[]"},{"name":"cids","type":"string[]"},{"name":"pools","type":"uint256[]"},{"name":"deadlines","type":"uint256[]"},{"name":"resolved","type":"bool[]"},{"name":"refunded","type":"bool[]"},{"name":"winners","type":"uint8[]"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":true,"inputs":[{"name":"user","type":"address"}],"name":"getAllGamesExtraData","outputs":[{"name":"resNO","type":"uint256[]"},{"name":"resYES","type":"uint256[]"},{"name":"myYES","type":"uint256[]"},{"name":"myNO","type":"uint256[]"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":true,"inputs":[{"name":"id","type":"uint256"}],"name":"getGameInfo","outputs":[{"name":"ipfsCID","type":"string"},{"name":"totalPool","type":"uint256"},{"name":"isResolved","type":"bool"},{"name":"winningOption","type":"uint8"},{"name":"deadlineSec","type":"uint256"},{"name":"isRefunded","type":"bool"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":true,"inputs":[{"name":"id","type":"uint256"},{"name":"user","type":"address"}],"name":"getGameExtraData","outputs":[{"name":"virtualReserves","type":"uint256[]"},{"name":"myShares","type":"uint256[]"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":false,"inputs":[{"name":"gameId","type":"uint256"},{"name":"optionId","type":"uint8"}],"name":"buyShares","outputs":[],"payable":true,"stateMutability":"payable","type":"function"}
]`

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

func EncodeGetGameInfo(gameID int) (string, error) {
	packed, err := parsedABI.Pack("getGameInfo", big.NewInt(int64(gameID)))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func DecodeGetGameInfo(gameID int, hexResult string) (*GameInfo, error) {
	results, err := parsedABI.Unpack("getGameInfo", fromHex(hexResult))
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getGameInfo: %w", err)
	}
	if len(results) < 6 {
		return nil, fmt.Errorf("unexpected getGameInfo results: %d", len(results))
	}

	info := &GameInfo{ID: gameID}
	var ok bool
	if info.IPFSCID, ok = results[0].(string); !ok {
		return nil, fmt.Errorf("ipfsCID is not string")
	}
	if info.TotalPool, ok = results[1].(*big.Int); !ok {
		return nil, fmt.Errorf("totalPool is not *big.Int")
	}
	if info.IsResolved, ok = results[2].(bool); !ok {
		return nil, fmt.Errorf("isResolved is not bool")
	}
	winner, ok := results[3].(uint8)
	if !ok {
		return nil, fmt.Errorf("winningOption is not uint8")
	}
	info.WinningOption = int(winner)
	deadline, ok := results[4].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("deadlineSec is not *big.Int")
	}
	info.DeadlineRaw = deadline.Int64()
	if info.IsRefunded, ok = results[5].(bool); !ok {
		return nil, fmt.Errorf("isRefunded is not bool")
	}
	return info, nil
}

func EncodeGetGameExtraData(gameID int, userAddress string) (string, error) {
	if !common.IsHexAddress(userAddress) {
		return "", fmt.Errorf("invalid user address: %s", userAddress)
	}
	packed, err := parsedABI.Pack("getGameExtraData", big.NewInt(int64(gameID)), common.HexToAddress(userAddress))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

// EncodeGetAllGamesExtraData encodes a call to getAllGamesExtraData(address user).
// This returns reserves for ALL games in a single eth_call, avoiding N+1
// per-game calls and dramatically reducing chain round-trips.
func EncodeGetAllGamesExtraData(userAddress string) (string, error) {
	if !common.IsHexAddress(userAddress) {
		return "", fmt.Errorf("invalid user address: %s", userAddress)
	}
	packed, err := parsedABI.Pack("getAllGamesExtraData", common.HexToAddress(userAddress))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func DecodeGetGameExtraData(hexResult string) (*GameExtraData, error) {
	results, err := parsedABI.Unpack("getGameExtraData", fromHex(hexResult))
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getGameExtraData: %w", err)
	}
	if len(results) < 2 {
		return nil, fmt.Errorf("unexpected getGameExtraData results: %d", len(results))
	}
	reserves, ok := results[0].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("virtualReserves is not []*big.Int")
	}
	shares, ok := results[1].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("myShares is not []*big.Int")
	}
	return &GameExtraData{
		VirtualReservesNOYES: normalizePair(reserves),
		MySharesYESNO:        normalizePair(shares),
	}, nil
}

// DecodeGetAllGamesExtraData unpacks the result of getAllGamesExtraData(address).
// Returns four parallel arrays (resNO, resYES, myYES, myNO) indexed in the
// same order as the games from getAllGames.
func DecodeGetAllGamesExtraData(hexResult string) (*AllGamesExtraData, error) {
	results, err := parsedABI.Unpack("getAllGamesExtraData", fromHex(hexResult))
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getAllGamesExtraData: %w", err)
	}
	if len(results) < 4 {
		return nil, fmt.Errorf("unexpected getAllGamesExtraData results: %d", len(results))
	}
	resNO, ok := results[0].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("resNO is not []*big.Int")
	}
	resYES, ok := results[1].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("resYES is not []*big.Int")
	}
	myYES, ok := results[2].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("myYES is not []*big.Int")
	}
	myNO, ok := results[3].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("myNO is not []*big.Int")
	}
	return &AllGamesExtraData{
		ResNO:       resNO,
		ResYES:      resYES,
		MySharesYES: myYES,
		MySharesNO:  myNO,
	}, nil
}

func EncodeBuyShares(gameID int, optionID int) (string, error) {
	if optionID < 0 || optionID > 1 {
		return "", fmt.Errorf("invalid option id: %d", optionID)
	}
	packed, err := parsedABI.Pack("buyShares", big.NewInt(int64(gameID)), uint8(optionID))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func fromHex(hexStr string) []byte {
	hexStr = strings.TrimPrefix(strings.TrimSpace(hexStr), "0x")
	b, _ := hex.DecodeString(hexStr)
	return b
}

func normalizePair(values []*big.Int) []*big.Int {
	out := []*big.Int{big.NewInt(0), big.NewInt(0)}
	for i := 0; i < len(values) && i < 2; i++ {
		if values[i] != nil {
			out[i] = new(big.Int).Set(values[i])
		}
	}
	return out
}
