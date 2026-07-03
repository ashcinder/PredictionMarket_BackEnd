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

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type rpcClient struct {
	url    string
	client *http.Client
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *rpcClient) call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	payload, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("%s RPC error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return fmt.Errorf("%s returned no result", method)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *rpcClient) quantity(ctx context.Context, method string, params []interface{}) (*big.Int, error) {
	var encoded string
	if err := c.call(ctx, method, params, &encoded); err != nil {
		return nil, err
	}
	value := new(big.Int)
	if _, ok := value.SetString(strings.TrimPrefix(encoded, "0x"), 16); !ok {
		return nil, fmt.Errorf("%s returned invalid quantity %q", method, encoded)
	}
	return value, nil
}

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:42515", "Supervisor JSON-RPC URL")
	bytecodePath := flag.String("bytecode", "", "compiled contract .bin file")
	gasLimit := flag.Uint64("gas", 15_000_000, "deployment gas limit")
	addressOnly := flag.Bool("address-only", false, "print the deployer address without sending a transaction")
	flag.Parse()

	privateKeyHex := strings.TrimPrefix(strings.TrimSpace(os.Getenv("LOCAL_DEPLOYER_PRIVATE_KEY")), "0x")
	if privateKeyHex == "" {
		exit(errors.New("LOCAL_DEPLOYER_PRIVATE_KEY is required"))
	}
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		exit(fmt.Errorf("invalid deployer private key: %w", err))
	}
	deployer := crypto.PubkeyToAddress(privateKey.PublicKey)
	if *addressOnly {
		fmt.Println(deployer.Hex())
		return
	}
	if strings.TrimSpace(*bytecodePath) == "" {
		exit(errors.New("-bytecode is required"))
	}

	encodedBytecode, err := os.ReadFile(*bytecodePath)
	if err != nil {
		exit(fmt.Errorf("read bytecode: %w", err))
	}
	bytecode, err := hex.DecodeString(strings.TrimSpace(string(encodedBytecode)))
	if err != nil {
		exit(fmt.Errorf("decode bytecode: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := &rpcClient{
		url:    *rpcURL,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	chainID, err := client.quantity(ctx, "eth_chainId", nil)
	if err != nil {
		exit(err)
	}
	nonce, err := client.quantity(ctx, "eth_getTransactionCount",
		[]interface{}{deployer.Hex(), "pending"})
	if err != nil {
		exit(err)
	}
	gasPrice, err := client.quantity(ctx, "eth_gasPrice", nil)
	if err != nil {
		exit(err)
	}

	unsigned := types.NewContractCreation(
		nonce.Uint64(),
		new(big.Int),
		*gasLimit,
		gasPrice,
		bytecode,
	)
	signed, err := types.SignTx(unsigned, types.NewLondonSigner(chainID), privateKey)
	if err != nil {
		exit(fmt.Errorf("sign deployment transaction: %w", err))
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		exit(fmt.Errorf("encode deployment transaction: %w", err))
	}
	var txHash string
	if err := client.call(ctx, "eth_sendRawTransaction",
		[]interface{}{"0x" + hex.EncodeToString(raw)}, &txHash); err != nil {
		exit(err)
	}
	fmt.Printf("deployment submitted: tx=%s deployer=%s\n", txHash, deployer.Hex())

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			exit(fmt.Errorf("wait for deployment receipt: %w", ctx.Err()))
		case <-ticker.C:
			var receipt struct {
				Status          string `json:"status"`
				ContractAddress string `json:"contractAddress"`
			}
			if err := client.call(ctx, "eth_getTransactionReceipt", []interface{}{txHash}, &receipt); err != nil {
				continue
			}
			if receipt.Status == "0x0" {
				exit(errors.New("deployment transaction reverted"))
			}
			if receipt.Status == "0x1" && receipt.ContractAddress != "" {
				fmt.Printf("contract deployed: %s\n", receipt.ContractAddress)
				return
			}
		}
	}
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, "deploylocal:", err)
	os.Exit(1)
}
