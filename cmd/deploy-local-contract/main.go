// Command deploy-local-contract deploys EVM creation bytecode to the local
// Supervisor JSON-RPC node using the configured operator key. It never writes
// application configuration; the caller must verify the printed address before
// switching services to it.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"PredictionMarket/internal/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type rpcClient struct {
	url    string
	client *http.Client
}

func main() {
	configPath := flag.String("config", "config.yaml", "backend YAML configuration")
	bytecodePath := flag.String("bytecode", "", "path to compiled constructor bytecode (.bin)")
	flag.Parse()
	if strings.TrimSpace(*bytecodePath) == "" {
		fatal(errors.New("-bytecode is required"))
	}
	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fatal(err)
	}
	if cfg.UseBrokerChain {
		fatal(errors.New("deploy-local-contract only supports chain.use_broker_chain=false"))
	}
	bytecode, err := os.ReadFile(*bytecodePath)
	if err != nil {
		fatal(fmt.Errorf("read bytecode: %w", err))
	}
	code, err := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(string(bytecode), "0x")))
	if err != nil || len(code) == 0 {
		fatal(errors.New("compiled bytecode is empty or invalid hex"))
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKey, "0x"))
	if err != nil {
		fatal(fmt.Errorf("read operator key: %w", err))
	}
	from := crypto.PubkeyToAddress(key.PublicKey).Hex()
	rpc := &rpcClient{url: cfg.RPCURL, client: &http.Client{Timeout: 30 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	chainID, err := rpc.quantity(ctx, "eth_chainId", nil)
	if err != nil {
		fatal(err)
	}
	nonce, err := rpc.quantity(ctx, "eth_getTransactionCount", []any{from, "pending"})
	if err != nil {
		fatal(err)
	}
	gasPrice, err := rpc.quantity(ctx, "eth_gasPrice", nil)
	if err != nil {
		fatal(err)
	}
	tx := types.NewContractCreation(nonce.Uint64(), big.NewInt(0), 8_000_000, gasPrice, code)
	signed, err := types.SignTx(tx, types.NewLondonSigner(chainID), key)
	if err != nil {
		fatal(fmt.Errorf("sign deployment: %w", err))
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		fatal(fmt.Errorf("encode deployment: %w", err))
	}
	var txHash string
	if err := rpc.call(ctx, "eth_sendRawTransaction", []any{"0x" + hex.EncodeToString(raw)}, &txHash); err != nil {
		fatal(fmt.Errorf("submit deployment: %w", err))
	}
	if !isTransactionHash(txHash) {
		fatal(errors.New("node returned an invalid deployment transaction hash"))
	}
	address, err := rpc.waitForContractReceipt(ctx, txHash)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("deployment_tx=%s\ncontract_address=%s\noperator=%s\n", txHash, address, from)
}

func (r *rpcClient) quantity(ctx context.Context, method string, params []any) (*big.Int, error) {
	var value string
	if err := r.call(ctx, method, params, &value); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	n := new(big.Int)
	if _, ok := n.SetString(strings.TrimPrefix(value, "0x"), 16); !ok {
		return nil, fmt.Errorf("%s returned invalid quantity %q", method, value)
	}
	return n, nil
}

func (r *rpcClient) waitForContractReceipt(ctx context.Context, txHash string) (string, error) {
	for {
		var receipt struct {
			Status          string `json:"status"`
			ContractAddress string `json:"contractAddress"`
		}
		err := r.call(ctx, "eth_getTransactionReceipt", []any{txHash}, &receipt)
		if err == nil && receipt.Status != "" {
			if receipt.Status != "0x1" {
				return "", errors.New("deployment transaction reverted")
			}
			if !common.IsHexAddress(receipt.ContractAddress) {
				return "", fmt.Errorf("receipt did not include a valid contract address: %q", receipt.ContractAddress)
			}
			return common.HexToAddress(receipt.ContractAddress).Hex(), nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for deployment receipt: %w", ctx.Err())
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func (r *rpcClient) call(ctx context.Context, method string, params []any, out any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params, "id": 1})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("rpc error: %s", envelope.Error.Message)
	}
	if len(envelope.Result) == 0 {
		return errors.New("response contains neither result nor error")
	}
	return json.Unmarshal(envelope.Result, out)
}

func isTransactionHash(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "deploy-local-contract:", err)
	os.Exit(1)
}
