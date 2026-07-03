package chain

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestBrokerPostRetriesGatewayTimeout(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte(`{"result":"0x1234"}`))
	}))
	defer server.Close()

	client, err := NewClient(
		hexutil.Encode(crypto.FromECDSA(key)),
		common.HexToAddress("0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c").Hex(),
		"",
		server.URL,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	restore := setBrokerRetryBackoffForTest(t, 0)
	defer restore()

	got, err := client.post(context.Background(), "eth_call", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"result":"0x1234"}` || attempts != 2 {
		t.Fatalf("got body=%q attempts=%d, want successful retry on second attempt", got, attempts)
	}
}

func TestExtractHexResultFromBrokerTransactionEnvelope(t *testing.T) {
	const want = "0x454821efbbf2e057f3955fc987409b25d2a2c584a4de7a392fe04a8cf8804195"
	got := extractHexResult(`{"jsonrpc":"2.0","id":1,"result":"` + want + `"}`)
	if got != want {
		t.Fatalf("unexpected tx hash: got %q want %q", got, want)
	}
}

func setBrokerRetryBackoffForTest(t *testing.T, delay time.Duration) func() {
	t.Helper()
	oldBackoff := brokerRetryBackoff
	oldLimiter := defaultBrokerLimiter
	brokerRetryBackoff = []time.Duration{delay}
	defaultBrokerLimiter = newBrokerRequestLimiter(1, 0)
	return func() {
		brokerRetryBackoff = oldBackoff
		defaultBrokerLimiter = oldLimiter
	}
}

func TestBrokerPostHonorsContextCancellation(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"result":"0x1234"}`))
	}))
	defer server.Close()

	client, err := NewClient(
		hexutil.Encode(crypto.FromECDSA(key)),
		common.HexToAddress("0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c").Hex(),
		"",
		server.URL,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.post(ctx, "eth_call", []byte(`{}`)); err == nil {
		t.Fatal("expected canceled context to stop broker request")
	}
}

func TestLocalSendTransactionSignsAndUsesRawRPC(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	var methods []string
	var rawTransaction string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int               `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		methods = append(methods, request.Method)
		result := interface{}("0x0")
		switch request.Method {
		case "eth_chainId":
			result = "0x41b"
		case "eth_getTransactionCount":
			result = "0x7"
		case "eth_gasPrice":
			result = "0x3b9aca00"
		case "eth_sendRawTransaction":
			if len(request.Params) != 1 {
				t.Fatalf("eth_sendRawTransaction params=%d, want 1", len(request.Params))
			}
			if err := json.Unmarshal(request.Params[0], &rawTransaction); err != nil {
				t.Fatal(err)
			}
			result = "0xabc123"
		case "eth_getTransactionReceipt":
			result = map[string]string{"status": "0x1"}
		default:
			t.Fatalf("unexpected RPC method %q", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  result,
		})
	}))
	defer server.Close()

	contract := common.HexToAddress("0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c")
	client, err := NewClient(
		hexutil.Encode(crypto.FromECDSA(key)),
		contract.Hex(),
		server.URL,
		"https://unused.example",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := client.SendTransaction(context.Background(), "0x010203", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0xabc123" {
		t.Fatalf("tx hash=%q, want 0xabc123", got)
	}
	wantMethods := "eth_chainId,eth_getTransactionCount,eth_gasPrice,eth_sendRawTransaction,eth_getTransactionReceipt"
	if strings.Join(methods, ",") != wantMethods {
		t.Fatalf("methods=%v, want %s", methods, wantMethods)
	}

	var signed types.Transaction
	if err := signed.UnmarshalBinary(common.FromHex(rawTransaction)); err != nil {
		t.Fatalf("decode raw transaction: %v", err)
	}
	from, err := types.Sender(types.NewLondonSigner(bigInt(1051)), &signed)
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	if from != crypto.PubkeyToAddress(key.PublicKey) {
		t.Fatalf("sender=%s, want %s", from.Hex(), crypto.PubkeyToAddress(key.PublicKey).Hex())
	}
	if signed.Nonce() != 7 || signed.To() == nil || *signed.To() != contract {
		t.Fatalf("unexpected signed transaction nonce=%d to=%v", signed.Nonce(), signed.To())
	}
}

func TestLocalEthCallIncludesZeroValueForSupervisor(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int               `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "eth_call" || len(request.Params) != 2 {
			t.Fatalf("unexpected request: method=%s params=%v", request.Method, request.Params)
		}
		var call map[string]interface{}
		if err := json.Unmarshal(request.Params[0], &call); err != nil {
			t.Fatal(err)
		}
		if call["value"] != "0x0" {
			t.Fatalf("eth_call value=%v, want 0x0", call["value"])
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  "0x1234",
		})
	}))
	defer server.Close()

	client, err := NewClient(
		hexutil.Encode(crypto.FromECDSA(key)),
		common.HexToAddress("0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c").Hex(),
		server.URL,
		"https://unused.example",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.EthCall(context.Background(), "0xdb1c45f9")
	if err != nil {
		t.Fatal(err)
	}
	if result != "0x1234" {
		t.Fatalf("result=%q, want 0x1234", result)
	}
}

func bigInt(value int64) *big.Int {
	return big.NewInt(value)
}
