package chain

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	localCallGasLimit  = 5_000_000
	localWriteGasLimit = 8_000_000
)

var (
	defaultBrokerLimiter = newBrokerRequestLimiter(1, time.Second)
	brokerRetryBackoff   = []time.Duration{2 * time.Second, 10 * time.Second}
)

type Client struct {
	privateKey      *ecdsa.PrivateKey
	walletAddress   string
	contractAddress string
	useBrokerChain  bool
	brokerBaseURL   string
	rpcURL          string
	httpClient      *http.Client
}

func NewClient(privateKeyHex, contractAddress, rpcURL, brokerBaseURL string, useBrokerChain bool) (*Client, error) {
	keyHex := strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x")
	key, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	return &Client{
		privateKey:      key,
		walletAddress:   crypto.PubkeyToAddress(key.PublicKey).Hex(),
		contractAddress: strings.ToLower(contractAddress),
		useBrokerChain:  useBrokerChain,
		brokerBaseURL:   strings.TrimSuffix(brokerBaseURL, "/") + "/",
		rpcURL:          rpcURL,
		httpClient:      &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (c *Client) WalletAddress() string {
	return c.walletAddress
}

func (c *Client) EthCall(ctx context.Context, data string) (string, error) {
	if c.useBrokerChain {
		return c.brokerEthCall(ctx, data)
	}
	msg := map[string]any{
		"from":  c.walletAddress,
		"to":    c.contractAddress,
		"data":  data,
		"gas":   fmt.Sprintf("0x%x", localCallGasLimit),
		"value": "0x0",
	}
	var result string
	if err := c.rpcCall(ctx, "eth_call", []any{msg, "latest"}, &result); err != nil {
		return "", err
	}
	if result == "" || result == "0x" {
		return "", fmt.Errorf("empty eth_call result")
	}
	return result, nil
}

func (c *Client) SendTransaction(ctx context.Context, data string, value *big.Int) (string, error) {
	if c.useBrokerChain {
		return c.brokerSendTransaction(ctx, data, value)
	}
	if value == nil {
		value = new(big.Int)
	}
	chainID, err := c.rpcBigInt(ctx, "eth_chainId", nil)
	if err != nil {
		return "", fmt.Errorf("get local chain id: %w", err)
	}
	nonce, err := c.rpcBigInt(ctx, "eth_getTransactionCount", []any{c.walletAddress, "pending"})
	if err != nil {
		return "", fmt.Errorf("get local account nonce: %w", err)
	}
	gasPrice, err := c.rpcBigInt(ctx, "eth_gasPrice", nil)
	if err != nil {
		return "", fmt.Errorf("get local gas price: %w", err)
	}
	callData, err := hex.DecodeString(strings.TrimPrefix(data, "0x"))
	if err != nil {
		return "", fmt.Errorf("decode transaction data: %w", err)
	}
	unsigned := types.NewTransaction(nonce.Uint64(), common.HexToAddress(c.contractAddress), value, localWriteGasLimit, gasPrice, callData)
	signed, err := types.SignTx(unsigned, types.NewLondonSigner(chainID), c.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign local transaction: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("encode local transaction: %w", err)
	}
	var txHash string
	if err := c.rpcCall(ctx, "eth_sendRawTransaction", []any{"0x" + hex.EncodeToString(raw)}, &txHash); err != nil {
		return "", err
	}
	if txHash == "" {
		return "", fmt.Errorf("local rpc returned empty tx hash")
	}
	if err := c.waitForReceipt(ctx, txHash); err != nil {
		return txHash, err
	}
	return txHash, nil
}

func (c *Client) CreateGame(ctx context.Context, ipfsCID string, durationSec int64, initialLiquidityWei *big.Int) (string, error) {
	data, err := EncodeCreateGame(ipfsCID, durationSec)
	if err != nil {
		return "", err
	}
	return c.SendTransaction(ctx, data, initialLiquidityWei)
}

func (c *Client) BuyShares(ctx context.Context, gameID int, optionID int, amountWei *big.Int) (string, error) {
	data, err := EncodeBuyShares(gameID, optionID)
	if err != nil {
		return "", err
	}
	return c.SendTransaction(ctx, data, amountWei)
}

func (c *Client) SendNativeTransfer(ctx context.Context, toAddress string, value *big.Int) (string, error) {
	if !common.IsHexAddress(toAddress) {
		return "", fmt.Errorf("recipient address is invalid")
	}
	if value == nil || value.Sign() <= 0 {
		return "", fmt.Errorf("transfer value must be positive")
	}
	if c.useBrokerChain {
		return c.brokerSendTo(ctx, common.HexToAddress(toAddress).Hex(), "0x", value, "0x5208")
	}
	chainID, err := c.rpcBigInt(ctx, "eth_chainId", nil)
	if err != nil {
		return "", fmt.Errorf("get local chain id: %w", err)
	}
	nonce, err := c.rpcBigInt(ctx, "eth_getTransactionCount", []any{c.walletAddress, "pending"})
	if err != nil {
		return "", fmt.Errorf("get local account nonce: %w", err)
	}
	gasPrice, err := c.rpcBigInt(ctx, "eth_gasPrice", nil)
	if err != nil {
		return "", fmt.Errorf("get local gas price: %w", err)
	}
	unsigned := types.NewTransaction(nonce.Uint64(), common.HexToAddress(toAddress), value, 21_000, gasPrice, nil)
	signed, err := types.SignTx(unsigned, types.NewLondonSigner(chainID), c.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign local transfer: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("encode local transfer: %w", err)
	}
	var txHash string
	if err := c.rpcCall(ctx, "eth_sendRawTransaction", []any{"0x" + hex.EncodeToString(raw)}, &txHash); err != nil {
		return "", err
	}
	if txHash == "" {
		return "", fmt.Errorf("local rpc returned empty transfer tx hash")
	}
	if err := c.waitForReceipt(ctx, txHash); err != nil {
		return txHash, err
	}
	return txHash, nil
}

func (c *Client) GameCount(ctx context.Context) (int, error) {
	data, err := EncodeGameCount()
	if err != nil {
		return 0, err
	}
	raw, err := c.EthCall(ctx, data)
	if err != nil {
		return 0, err
	}
	return DecodeGameCount(raw)
}

func (c *Client) GetGameInfo(ctx context.Context, gameID int) (*GameInfo, error) {
	data, err := EncodeGetGameInfo(gameID)
	if err != nil {
		return nil, err
	}
	raw, err := c.EthCall(ctx, data)
	if err != nil {
		return nil, err
	}
	return DecodeGetGameInfo(gameID, raw)
}

func (c *Client) GetGameExtraData(ctx context.Context, gameID int, userAddress string) (*GameExtraData, error) {
	data, err := EncodeGetGameExtraData(gameID, userAddress)
	if err != nil {
		return nil, err
	}
	raw, err := c.EthCall(ctx, data)
	if err != nil {
		return nil, err
	}
	return DecodeGetGameExtraData(raw)
}

func (c *Client) rpcBigInt(ctx context.Context, method string, params []any) (*big.Int, error) {
	var encoded string
	if err := c.rpcCall(ctx, method, params, &encoded); err != nil {
		return nil, err
	}
	value := new(big.Int)
	if _, ok := value.SetString(strings.TrimPrefix(encoded, "0x"), 16); !ok {
		return nil, fmt.Errorf("%s returned invalid hex quantity %q", method, encoded)
	}
	return value, nil
}

func (c *Client) rpcCall(ctx context.Context, method string, params []any, out any) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("rpc decode: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc error: %s", envelope.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *Client) waitForReceipt(ctx context.Context, txHash string) error {
	for i := 0; i < 12; i++ {
		var receipt struct {
			Status string `json:"status"`
		}
		err := c.rpcCall(ctx, "eth_getTransactionReceipt", []any{txHash}, &receipt)
		if err == nil && receipt.Status != "" {
			if receipt.Status == "0x0" {
				return fmt.Errorf("transaction reverted on chain")
			}
			return nil
		}
		if err := sleepWithContext(ctx, 2500*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("transaction not confirmed within timeout")
}

func (c *Client) brokerEthCall(ctx context.Context, data string) (string, error) {
	randomStr := randomUUID()
	value := "0x0"
	to := c.contractAddress
	sign1, sign2, err := signECDSA(c.privateKey, to+data+value+randomStr)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(callReq{
		PublicKey: publicKeyHex(c.privateKey),
		RandomStr: randomStr,
		To:        to,
		Data:      data,
		Value:     value,
		Sign1:     sign1,
		Sign2:     sign2,
	})
	if err != nil {
		return "", err
	}
	resp, err := c.post(ctx, "eth_call", body)
	if err != nil {
		return "", err
	}
	return extractHexResult(resp), nil
}

func (c *Client) brokerSendTransaction(ctx context.Context, data string, value *big.Int) (string, error) {
	return c.brokerSendTo(ctx, c.contractAddress, data, value, "0x7a1200")
}

func (c *Client) brokerSendTo(ctx context.Context, to string, data string, value *big.Int, gas string) (string, error) {
	randomStr := randomUUID()
	if strings.TrimSpace(data) == "" {
		data = "0x"
	}
	if strings.TrimSpace(gas) == "" {
		gas = "0x7a1200"
	}
	valueHex := "0x0"
	if value != nil && value.Sign() > 0 {
		valueHex = "0x" + value.Text(16)
	}
	sign1, sign2, err := signECDSA(c.privateKey, to+data+valueHex+gas+randomStr)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(sendTxReq{
		PublicKey: publicKeyHex(c.privateKey),
		RandomStr: randomStr,
		To:        to,
		Data:      data,
		Value:     valueHex,
		Gas:       gas,
		Sign1:     sign1,
		Sign2:     sign2,
	})
	if err != nil {
		return "", err
	}
	resp, err := c.post(ctx, "eth_sendTransaction", body)
	if err != nil {
		return "", err
	}
	if strings.Contains(strings.ToLower(resp), "error") || strings.Contains(strings.ToLower(resp), "failed") {
		return "", fmt.Errorf("broker chain tx failed: %s", resp)
	}
	txHash := extractHexResult(resp)
	if txHash == "" || txHash == "0x" {
		return "", fmt.Errorf("broker chain returned empty tx hash: %s", resp)
	}
	return txHash, nil
}

func (c *Client) post(ctx context.Context, endpoint string, body []byte) (string, error) {
	attempts := len(brokerRetryBackoff) + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, brokerRetryBackoff[attempt-1]); err != nil {
				return "", err
			}
		}
		resp, err := c.postOnce(ctx, endpoint, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryableBrokerError(err) {
			return "", err
		}
	}
	return "", lastErr
}

func (c *Client) postOnce(ctx context.Context, endpoint string, body []byte) (string, error) {
	release, err := defaultBrokerLimiter.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.brokerBaseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &BrokerHTTPError{Endpoint: endpoint, StatusCode: resp.StatusCode, Body: string(raw)}
	}
	return string(raw), nil
}

type BrokerHTTPError struct {
	Endpoint   string
	StatusCode int
	Body       string
}

func (e *BrokerHTTPError) Error() string {
	return fmt.Sprintf("broker chain %s: HTTP %d %s", e.Endpoint, e.StatusCode, e.Body)
}

func isRetryableBrokerError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var brokerErr *BrokerHTTPError
	if errors.As(err, &brokerErr) {
		switch brokerErr.StatusCode {
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type brokerRequestLimiter struct {
	sem         chan struct{}
	mu          sync.Mutex
	next        time.Time
	minInterval time.Duration
}

func newBrokerRequestLimiter(maxConcurrent int, minInterval time.Duration) *brokerRequestLimiter {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &brokerRequestLimiter{
		sem:         make(chan struct{}, maxConcurrent),
		minInterval: minInterval,
	}
}

func (l *brokerRequestLimiter) acquire(ctx context.Context) (func(), error) {
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-l.sem }
	l.mu.Lock()
	now := time.Now()
	wait := l.next.Sub(now)
	if wait > 0 {
		l.mu.Unlock()
		if err := sleepWithContext(ctx, wait); err != nil {
			release()
			return nil, err
		}
		l.mu.Lock()
		now = time.Now()
	}
	if l.minInterval > 0 {
		l.next = now.Add(l.minInterval)
	}
	l.mu.Unlock()
	return release, nil
}

type callReq struct {
	PublicKey string `json:"PublicKey"`
	RandomStr string `json:"RandomStr"`
	To        string `json:"To"`
	Data      string `json:"data"`
	Value     string `json:"value"`
	Sign1     string `json:"Sign1"`
	Sign2     string `json:"Sign2"`
}

type sendTxReq struct {
	PublicKey string `json:"PublicKey"`
	RandomStr string `json:"RandomStr"`
	To        string `json:"To"`
	Data      string `json:"data"`
	Value     string `json:"value"`
	Gas       string `json:"Gas"`
	Sign1     string `json:"Sign1"`
	Sign2     string `json:"Sign2"`
}

func signECDSA(key *ecdsa.PrivateKey, data string) (string, string, error) {
	hash := sha256.Sum256([]byte(data))
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(r.Bytes()), hex.EncodeToString(s.Bytes()), nil
}

func publicKeyHex(key *ecdsa.PrivateKey) string {
	return hex.EncodeToString(crypto.FromECDSAPub(&key.PublicKey))
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func extractHexResult(response string) string {
	response = strings.TrimSpace(response)
	if response == "" {
		return "0x"
	}
	if strings.HasPrefix(response, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(response), &obj); err == nil {
			if value, ok := obj["result"].(string); ok {
				return value
			}
			if value, ok := obj["data"].(string); ok {
				return value
			}
		}
	}
	if strings.Contains(strings.ToLower(response), "reverted") {
		return "0x"
	}
	return response
}
