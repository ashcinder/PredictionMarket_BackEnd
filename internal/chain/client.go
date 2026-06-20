package chain

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/sha3"
)

const (
	localCallGasLimit  = 5_000_000
	localWriteGasLimit = 8_000_000
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
		httpClient:      &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *Client) WalletAddress() string {
	return c.walletAddress
}

func (c *Client) Close() {}

func (c *Client) EthCall(ctx context.Context, data string) (string, error) {
	if c.useBrokerChain {
		return c.brokerEthCall(data)
	}
	msg := map[string]interface{}{
		"from": c.walletAddress,
		"to":   c.contractAddress,
		"data": data,
		"gas":  fmt.Sprintf("0x%x", localCallGasLimit),
	}
	var result string
	if err := c.rpcCall(ctx, "eth_call", []interface{}{msg, "latest"}, &result); err != nil {
		return "", err
	}
	if result == "" || result == "0x" {
		return "", fmt.Errorf("empty eth_call result")
	}
	return result, nil
}

func (c *Client) SendTransaction(ctx context.Context, data string, value *big.Int) (string, error) {
	if c.useBrokerChain {
		return c.brokerSendTransaction(data, value)
	}
	valueHex := "0x0"
	if value != nil && value.Sign() > 0 {
		valueHex = "0x" + value.Text(16)
	}
	msg := map[string]interface{}{
		"from":  c.walletAddress,
		"to":    c.contractAddress,
		"data":  data,
		"value": valueHex,
		"gas":   fmt.Sprintf("0x%x", localWriteGasLimit),
	}
	var txHash string
	if err := c.rpcCall(ctx, "eth_sendTransaction", []interface{}{msg}, &txHash); err != nil {
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

func (c *Client) rpcCall(ctx context.Context, method string, params []interface{}, out interface{}) error {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	body, err := json.Marshal(payload)
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
		err := c.rpcCall(ctx, "eth_getTransactionReceipt", []interface{}{txHash}, &receipt)
		if err == nil && receipt.Status != "" {
			if receipt.Status == "0x0" {
				return fmt.Errorf("transaction reverted on chain")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2500 * time.Millisecond):
		}
	}
	return fmt.Errorf("transaction not confirmed within timeout")
}

func (c *Client) brokerEthCall(data string) (string, error) {
	randomStr := randomUUID()
	value := "0x0"
	to := c.contractAddress
	signData := to + data + value + randomStr
	sign1, sign2, err := signECDSA(c.privateKey, signData)
	if err != nil {
		return "", err
	}
	req := callReq{
		PublicKey: publicKeyHex(c.privateKey),
		RandomStr: randomStr,
		To:        to,
		Data:      data,
		Value:     value,
		Sign1:     sign1,
		Sign2:     sign2,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	resp, err := c.post("eth_call", body)
	if err != nil {
		return "", err
	}
	return extractHexResult(resp), nil
}

func (c *Client) brokerSendTransaction(data string, value *big.Int) (string, error) {
	randomStr := randomUUID()
	gas := "0x7a1200"
	valueHex := "0x0"
	if value != nil && value.Sign() > 0 {
		valueHex = "0x" + value.Text(16)
	}
	to := c.contractAddress
	signData := to + data + valueHex + gas + randomStr
	sign1, sign2, err := signECDSA(c.privateKey, signData)
	if err != nil {
		return "", err
	}
	req := sendTxReq{
		PublicKey: publicKeyHex(c.privateKey),
		RandomStr: randomStr,
		To:        to,
		Data:      data,
		Value:     valueHex,
		Gas:       gas,
		Sign1:     sign1,
		Sign2:     sign2,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	resp, err := c.post("eth_sendTransaction", body)
	if err != nil {
		return "", err
	}
	if strings.Contains(strings.ToLower(resp), "error") || strings.Contains(strings.ToLower(resp), "failed") {
		return "", fmt.Errorf("broker chain tx failed: %s", resp)
	}
	return resp, nil
}

func (c *Client) post(endpoint string, body []byte) (string, error) {
	req, err := http.NewRequest(http.MethodPost, c.brokerBaseURL+endpoint, bytes.NewReader(body))
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
		return "", fmt.Errorf("broker chain %s: HTTP %d %s", endpoint, resp.StatusCode, string(raw))
	}
	return string(raw), nil
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
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(response), &obj); err == nil {
			if v, ok := obj["result"].(string); ok {
				return v
			}
			if v, ok := obj["data"].(string); ok {
				return v
			}
		}
	}
	if strings.Contains(strings.ToLower(response), "reverted") {
		return "0x"
	}
	return response
}

func selector(signature string) []byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte(signature))
	sum := h.Sum(nil)
	return sum[:4]
}

func encodeCall(signature string, args ...[]byte) string {
	var buf bytes.Buffer
	buf.Write(selector(signature))
	for _, arg := range args {
		buf.Write(arg)
	}
	return "0x" + hex.EncodeToString(buf.Bytes())
}

func EncodeGetAllGames() string {
	return encodeCall("getAllGames()")
}

func EncodeResolveGame(gameID int, winningOption int) string {
	idWord := padWord(big.NewInt(int64(gameID)).Bytes())
	optWord := padWord([]byte{byte(winningOption)})
	return encodeCall("resolveGame(uint256,uint8)", idWord, optWord)
}

func padWord(b []byte) []byte {
	word := make([]byte, 32)
	copy(word[32-len(b):], b)
	return word
}

type GameOnChain struct {
	ID            int
	IPFSCID       string
	TotalPool     *big.Int
	DeadlineRaw   int64
	IsResolved    bool
	IsRefunded    bool
	WinningOption int
}

func RemainingSecondsUntilDeadline(rawDeadline int64, nowMillis int64) int64 {
	if rawDeadline <= 0 {
		return 0
	}
	deadline := rawDeadline
	if rawDeadline <= 10_000_000_000 {
		deadline = rawDeadline * 1000
	}
	diff := deadline - nowMillis
	if diff <= 0 {
		return 0
	}
	return diff / 1000
}

func IsDeadlinePassed(rawDeadline int64, nowMillis int64) bool {
	return RemainingSecondsUntilDeadline(rawDeadline, nowMillis) <= 0
}
