package chain

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const contractABI = `[
{"constant":false,"inputs":[{"name":"_ipfsCID","type":"string"},{"name":"_durationSec","type":"uint256"}],"name":"createGame","outputs":[],"payable":true,"stateMutability":"payable","type":"function"},
{"constant":true,"inputs":[],"name":"gameCount","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":true,"inputs":[],"name":"getAllGames","outputs":[{"name":"ids","type":"uint256[]"},{"name":"cids","type":"string[]"},{"name":"pools","type":"uint256[]"},{"name":"deadlines","type":"uint256[]"},{"name":"resolved","type":"bool[]"},{"name":"refunded","type":"bool[]"},{"name":"winners","type":"uint8[]"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":true,"inputs":[{"name":"id","type":"uint256"}],"name":"getGameInfo","outputs":[{"name":"ipfsCID","type":"string"},{"name":"totalPool","type":"uint256"},{"name":"isResolved","type":"bool"},{"name":"winningOption","type":"uint8"},{"name":"deadlineSec","type":"uint256"},{"name":"isRefunded","type":"bool"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":true,"inputs":[{"name":"id","type":"uint256"},{"name":"user","type":"address"}],"name":"getGameExtraData","outputs":[{"name":"virtualReserves","type":"uint256[]"},{"name":"myShares","type":"uint256[]"}],"payable":false,"stateMutability":"view","type":"function"},
{"constant":false,"inputs":[{"name":"gameId","type":"uint256"},{"name":"optionId","type":"uint8"}],"name":"buyShares","outputs":[],"payable":true,"stateMutability":"payable","type":"function"}
]`

var parsedABI = mustParseABI()

type GameInfo struct {
	ID            int
	IPFSCID       string
	TotalPool     *big.Int
	IsResolved    bool
	WinningOption int
	DeadlineRaw   int64
	IsRefunded    bool
}

type GameExtraData struct {
	VirtualReservesNOYES []*big.Int
	MySharesYESNO        []*big.Int
}

func mustParseABI() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(contractABI))
	if err != nil {
		panic(err)
	}
	return parsed
}

func EncodeCreateGame(ipfsCID string, durationSec int64) (string, error) {
	if strings.TrimSpace(ipfsCID) == "" {
		return "", fmt.Errorf("ipfs cid is required")
	}
	if durationSec <= 0 {
		return "", fmt.Errorf("duration must be positive")
	}
	packed, err := parsedABI.Pack("createGame", ipfsCID, big.NewInt(durationSec))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func EncodeGameCount() (string, error) {
	packed, err := parsedABI.Pack("gameCount")
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func DecodeGameCount(hexResult string) (int, error) {
	results, err := parsedABI.Unpack("gameCount", fromHex(hexResult))
	if err != nil {
		return 0, fmt.Errorf("unpack gameCount: %w", err)
	}
	if len(results) != 1 {
		return 0, fmt.Errorf("unexpected gameCount results: %d", len(results))
	}
	count, ok := results[0].(*big.Int)
	if !ok {
		return 0, fmt.Errorf("gameCount result is not *big.Int")
	}
	return int(count.Int64()), nil
}

func EncodeGetGameInfo(gameID int) (string, error) {
	if gameID <= 0 {
		return "", fmt.Errorf("game id must be positive")
	}
	packed, err := parsedABI.Pack("getGameInfo", big.NewInt(int64(gameID)))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func DecodeGetGameInfo(gameID int, hexResult string) (*GameInfo, error) {
	results, err := parsedABI.Unpack("getGameInfo", fromHex(hexResult))
	if err != nil {
		return nil, fmt.Errorf("unpack getGameInfo: %w", err)
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
	if gameID <= 0 {
		return "", fmt.Errorf("game id must be positive")
	}
	if !common.IsHexAddress(userAddress) {
		return "", fmt.Errorf("user address is invalid")
	}
	packed, err := parsedABI.Pack("getGameExtraData", big.NewInt(int64(gameID)), common.HexToAddress(userAddress))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func DecodeGetGameExtraData(hexResult string) (*GameExtraData, error) {
	results, err := parsedABI.Unpack("getGameExtraData", fromHex(hexResult))
	if err != nil {
		return nil, fmt.Errorf("unpack getGameExtraData: %w", err)
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

func EncodeBuyShares(gameID int, optionID int) (string, error) {
	if gameID <= 0 {
		return "", fmt.Errorf("game id must be positive")
	}
	if optionID < 0 || optionID > 1 {
		return "", fmt.Errorf("invalid option id: %d", optionID)
	}
	packed, err := parsedABI.Pack("buyShares", big.NewInt(int64(gameID)), uint8(optionID))
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(packed), nil
}

func fromHex(value string) []byte {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	out, _ := hex.DecodeString(value)
	return out
}

func normalizePair(values []*big.Int) []*big.Int {
	out := []*big.Int{big.NewInt(0), big.NewInt(0)}
	for i := 0; i < len(out) && i < len(values); i++ {
		if values[i] != nil {
			out[i] = new(big.Int).Set(values[i])
		}
	}
	return out
}
