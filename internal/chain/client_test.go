package chain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
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
