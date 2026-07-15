package chainlinkfeed

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	latestRoundDataSelector = "0xfeaf968c"
	getRoundDataSelector    = "0x9a6fc8f5"
	decimalsSelector        = "0x313ce567"
)

var phaseAggregatorsSelector = "0x" + hex.EncodeToString(crypto.Keccak256([]byte("phaseAggregators(uint16)"))[:4])

type Round struct {
	Feed            common.Address
	ID              *big.Int
	Answer          *big.Int
	Decimals        uint8
	Price           float64
	StartedAt       time.Time
	UpdatedAt       time.Time
	AnsweredInRound *big.Int
}

type Client struct {
	rpcURLs    []string
	httpClient *http.Client
}

func NewClient(rpcURLs []string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	urls := make([]string, 0, len(rpcURLs))
	seen := make(map[string]bool, len(rpcURLs))
	for _, raw := range rpcURLs {
		url := strings.TrimSpace(raw)
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		urls = append(urls, url)
	}
	return &Client{rpcURLs: urls, httpClient: &http.Client{Timeout: timeout}}
}

func (c *Client) Available() bool {
	return c != nil && c.httpClient != nil && len(c.rpcURLs) > 0
}

func (c *Client) LatestRound(ctx context.Context, feed common.Address) (Round, error) {
	decimals, err := c.decimals(ctx, feed)
	if err != nil {
		return Round{}, err
	}
	return c.roundCall(ctx, feed, latestRoundDataSelector, decimals)
}

func (c *Client) Round(ctx context.Context, feed common.Address, roundID *big.Int) (Round, error) {
	if roundID == nil || roundID.Sign() <= 0 {
		return Round{}, errors.New("Chainlink round id must be positive")
	}
	decimals, err := c.decimals(ctx, feed)
	if err != nil {
		return Round{}, err
	}
	return c.roundWithDecimals(ctx, feed, roundID, decimals)
}

func (c *Client) RoundAtOrBefore(ctx context.Context, feed common.Address, boundary time.Time) (Round, error) {
	if !c.Available() {
		return Round{}, errors.New("Chainlink RPC is not configured")
	}
	if feed == (common.Address{}) {
		return Round{}, errors.New("Chainlink feed address is required")
	}
	if boundary.IsZero() {
		return Round{}, errors.New("Chainlink boundary is required")
	}
	decimals, err := c.decimals(ctx, feed)
	if err != nil {
		return Round{}, err
	}
	latest, err := c.roundCall(ctx, feed, latestRoundDataSelector, decimals)
	if err != nil {
		return Round{}, err
	}
	phase, latestAggregatorRound := splitCompositeRoundID(latest.ID)
	if phase == 0 || latestAggregatorRound == 0 {
		return Round{}, fmt.Errorf("Chainlink latest round %s has invalid phase", latest.ID)
	}

	for currentPhase := phase; currentPhase > 0; currentPhase-- {
		maxRound := latestAggregatorRound
		if currentPhase != phase {
			maxRound, err = c.phaseLatestRound(ctx, feed, currentPhase)
			if err != nil {
				continue
			}
		}
		candidate, found, searchErr := c.roundAtOrBeforeInPhase(
			ctx, feed, currentPhase, maxRound, boundary.UTC(), decimals)
		if searchErr != nil {
			return Round{}, searchErr
		}
		if found {
			return candidate, nil
		}
	}
	return Round{}, fmt.Errorf("no Chainlink round for %s at or before %s", feed.Hex(), boundary.UTC().Format(time.RFC3339))
}

func (c *Client) roundAtOrBeforeInPhase(
	ctx context.Context,
	feed common.Address,
	phase uint16,
	maxAggregatorRound uint64,
	boundary time.Time,
	decimals uint8,
) (Round, bool, error) {
	if maxAggregatorRound == 0 {
		return Round{}, false, nil
	}
	latest, err := c.roundWithDecimals(ctx, feed, makeCompositeRoundID(phase, maxAggregatorRound), decimals)
	if err != nil {
		return Round{}, false, err
	}
	if !latest.UpdatedAt.After(boundary) {
		return latest, true, nil
	}

	upper := maxAggregatorRound
	step := uint64(1)
	var lower uint64
	var lowerRound Round
	for {
		candidateID := uint64(1)
		if step < maxAggregatorRound {
			candidateID = maxAggregatorRound - step
		}
		candidate, candidateErr := c.roundWithDecimals(
			ctx, feed, makeCompositeRoundID(phase, candidateID), decimals)
		if candidateErr != nil {
			return Round{}, false, candidateErr
		}
		if !candidate.UpdatedAt.After(boundary) {
			lower, lowerRound = candidateID, candidate
			break
		}
		upper = candidateID
		if candidateID == 1 {
			return Round{}, false, nil
		}
		if step > math.MaxUint64/2 {
			return Round{}, false, errors.New("Chainlink round search overflow")
		}
		step *= 2
	}

	for upper-lower > 1 {
		mid := lower + (upper-lower)/2
		candidate, candidateErr := c.roundWithDecimals(
			ctx, feed, makeCompositeRoundID(phase, mid), decimals)
		if candidateErr != nil {
			return Round{}, false, candidateErr
		}
		if candidate.UpdatedAt.After(boundary) {
			upper = mid
		} else {
			lower, lowerRound = mid, candidate
		}
	}
	return lowerRound, true, nil
}

func (c *Client) phaseLatestRound(ctx context.Context, proxy common.Address, phase uint16) (uint64, error) {
	arg := new(big.Int).SetUint64(uint64(phase)).FillBytes(make([]byte, 32))
	result, err := c.call(ctx, proxy, phaseAggregatorsSelector+hex.EncodeToString(arg))
	if err != nil {
		return 0, err
	}
	data, err := decodeHexResult(result)
	if err != nil || len(data) != 32 {
		return 0, fmt.Errorf("decode Chainlink phase %d aggregator: %w", phase, err)
	}
	aggregator := common.BytesToAddress(data[12:])
	if aggregator == (common.Address{}) {
		return 0, fmt.Errorf("Chainlink phase %d aggregator is empty", phase)
	}
	response, err := c.call(ctx, aggregator, latestRoundDataSelector)
	if err != nil {
		return 0, err
	}
	words, err := decodeWords(response, 5)
	if err != nil {
		return 0, err
	}
	return words[0].Uint64(), nil
}

func (c *Client) roundWithDecimals(
	ctx context.Context, feed common.Address, roundID *big.Int, decimals uint8,
) (Round, error) {
	argument := roundID.FillBytes(make([]byte, 32))
	return c.roundCall(ctx, feed, getRoundDataSelector+hex.EncodeToString(argument), decimals)
}

func (c *Client) roundCall(ctx context.Context, feed common.Address, data string, decimals uint8) (Round, error) {
	result, err := c.call(ctx, feed, data)
	if err != nil {
		return Round{}, err
	}
	words, err := decodeWords(result, 5)
	if err != nil {
		return Round{}, err
	}
	id := new(big.Int).Set(words[0])
	answer := signedWord(words[1])
	answeredInRound := new(big.Int).Set(words[4])
	if id.Sign() <= 0 || answer.Sign() <= 0 || words[3].Sign() <= 0 {
		return Round{}, errors.New("Chainlink round contains non-positive values")
	}
	if answeredInRound.Cmp(id) < 0 {
		return Round{}, errors.New("Chainlink answeredInRound is older than roundId")
	}
	price, _ := new(big.Float).Quo(
		new(big.Float).SetInt(answer),
		new(big.Float).SetFloat64(math.Pow10(int(decimals))),
	).Float64()
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return Round{}, errors.New("Chainlink round price is invalid")
	}
	return Round{
		Feed: feed, ID: id, Answer: answer, Decimals: decimals, Price: price,
		StartedAt:       time.Unix(words[2].Int64(), 0).UTC(),
		UpdatedAt:       time.Unix(words[3].Int64(), 0).UTC(),
		AnsweredInRound: answeredInRound,
	}, nil
}

func (c *Client) decimals(ctx context.Context, feed common.Address) (uint8, error) {
	result, err := c.call(ctx, feed, decimalsSelector)
	if err != nil {
		return 0, err
	}
	words, err := decodeWords(result, 1)
	if err != nil {
		return 0, err
	}
	if !words[0].IsUint64() || words[0].Uint64() > 18 {
		return 0, errors.New("Chainlink feed decimals are invalid")
	}
	return uint8(words[0].Uint64()), nil
}

func (c *Client) call(ctx context.Context, address common.Address, data string) (string, error) {
	if !c.Available() {
		return "", errors.New("Chainlink RPC is not configured")
	}
	if address == (common.Address{}) {
		return "", errors.New("Chainlink contract address is required")
	}
	var failures []string
	for _, rpcURL := range c.rpcURLs {
		result, err := c.callRPC(ctx, rpcURL, address, data)
		if err == nil {
			return result, nil
		}
		failures = append(failures, rpcURL+": "+err.Error())
	}
	return "", fmt.Errorf("all Chainlink RPC endpoints failed: %s", strings.Join(failures, "; "))
}

func (c *Client) callRPC(
	ctx context.Context, rpcURL string, address common.Address, data string,
) (string, error) {
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "eth_call",
		"params": []any{map[string]string{"to": address.Hex(), "data": data}, "latest"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PredictionMarket/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var response struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode JSON-RPC response: %w", err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("JSON-RPC %d: %s", response.Error.Code, response.Error.Message)
	}
	if strings.TrimSpace(response.Result) == "" {
		return "", errors.New("JSON-RPC response has no result")
	}
	return response.Result, nil
}

func decodeWords(raw string, count int) ([]*big.Int, error) {
	data, err := decodeHexResult(raw)
	if err != nil {
		return nil, err
	}
	if len(data) != count*32 {
		return nil, fmt.Errorf("Chainlink ABI result length %d, want %d", len(data), count*32)
	}
	words := make([]*big.Int, count)
	for index := range words {
		words[index] = new(big.Int).SetBytes(data[index*32 : (index+1)*32])
	}
	return words, nil
}

func decodeHexResult(raw string) ([]byte, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" || len(raw)%2 != 0 {
		return nil, errors.New("Chainlink ABI result is empty or malformed")
	}
	data, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode Chainlink ABI result: %w", err)
	}
	return data, nil
}

func signedWord(word *big.Int) *big.Int {
	value := new(big.Int).Set(word)
	if value.Bit(255) == 1 {
		value.Sub(value, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return value
}

func makeCompositeRoundID(phase uint16, aggregatorRound uint64) *big.Int {
	value := new(big.Int).Lsh(new(big.Int).SetUint64(uint64(phase)), 64)
	return value.Or(value, new(big.Int).SetUint64(aggregatorRound))
}

func splitCompositeRoundID(value *big.Int) (uint16, uint64) {
	if value == nil || value.Sign() <= 0 {
		return 0, 0
	}
	phase := new(big.Int).Rsh(new(big.Int).Set(value), 64).Uint64()
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	aggregatorRound := new(big.Int).And(new(big.Int).Set(value), mask).Uint64()
	return uint16(phase), aggregatorRound
}
