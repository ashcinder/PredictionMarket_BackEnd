package chainlinkfeed

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

var testFeed = common.HexToAddress("0x214eD9Da11D2fbe465a6fc601a91E62EbEc1a0D6")

func TestClientDecodesLatestRoundData(t *testing.T) {
	server := newRPCFixture(t, func(data string) (string, bool) {
		switch data {
		case decimalsSelector:
			return encodeWords(big.NewInt(8)), true
		case latestRoundDataSelector:
			return encodeRoundResult(compositeRoundID(5, 8286), 407910500000, 1784033519), true
		default:
			return "", false
		}
	})
	defer server.Close()

	client := NewClient([]string{server.URL}, time.Second)
	round, err := client.LatestRound(context.Background(), testFeed)
	if err != nil {
		t.Fatal(err)
	}
	if round.ID.String() != "92233720368547766366" {
		t.Fatalf("round id = %s", round.ID)
	}
	if round.Price != 4079.105 {
		t.Fatalf("price = %.8f", round.Price)
	}
	if got := round.UpdatedAt.Unix(); got != 1784033519 {
		t.Fatalf("updated_at = %d", got)
	}
}

func TestClientFailsOverToSecondRPC(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "limited", http.StatusTooManyRequests)
	}))
	defer bad.Close()
	good := newRPCFixture(t, func(data string) (string, bool) {
		if data == decimalsSelector {
			return encodeWords(big.NewInt(8)), true
		}
		if data == latestRoundDataSelector {
			return encodeRoundResult(compositeRoundID(5, 8286), 407910500000, 1784033519), true
		}
		return "", false
	})
	defer good.Close()

	client := NewClient([]string{bad.URL, good.URL}, time.Second)
	if _, err := client.LatestRound(context.Background(), testFeed); err != nil {
		t.Fatal(err)
	}
}

func TestRoundAtOrBeforeReturnsLastHistoricalRound(t *testing.T) {
	var historicalCalls atomic.Int32
	rounds := map[string]string{}
	for index, stamp := range []int64{1783900800, 1783987200, 1784073600, 1784160000} {
		id := compositeRoundID(5, uint64(100+index))
		rounds[getRoundDataSelector+fmt.Sprintf("%064x", id)] = encodeRoundResult(id, 400000000000+int64(index)*1000000000, stamp)
	}
	server := newRPCFixture(t, func(data string) (string, bool) {
		switch data {
		case decimalsSelector:
			return encodeWords(big.NewInt(8)), true
		case latestRoundDataSelector:
			return rounds[getRoundDataSelector+fmt.Sprintf("%064x", compositeRoundID(5, 103))], true
		default:
			if value, ok := rounds[data]; ok {
				historicalCalls.Add(1)
				return value, true
			}
			return "", false
		}
	})
	defer server.Close()

	client := NewClient([]string{server.URL}, time.Second)
	boundary := time.Unix(1784020000, 0).UTC()
	round, err := client.RoundAtOrBefore(context.Background(), testFeed, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if round.ID.Cmp(compositeRoundID(5, 101)) != 0 {
		t.Fatalf("round id = %s", round.ID)
	}
	if round.UpdatedAt.Unix() != 1783987200 {
		t.Fatalf("updated_at = %s", round.UpdatedAt)
	}
	if historicalCalls.Load() == 0 {
		t.Fatal("historical rounds were not queried")
	}
}

func newRPCFixture(t *testing.T, responder func(string) (string, bool)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			JSONRPC string            `json:"jsonrpc"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
			ID      int               `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.Method != "eth_call" || len(payload.Params) < 1 {
			http.Error(w, "unexpected rpc method", http.StatusBadRequest)
			return
		}
		var call struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(payload.Params[0], &call); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, ok := responder(strings.ToLower(call.Data))
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": payload.ID,
				"error": map[string]any{"code": 3, "message": "execution reverted"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": payload.ID, "result": "0x" + result,
		})
	}))
}

func encodeRoundResult(id *big.Int, answer, updatedAt int64) string {
	return encodeWords(id, big.NewInt(answer), big.NewInt(updatedAt-10), big.NewInt(updatedAt), id)
}

func encodeWords(values ...*big.Int) string {
	var output strings.Builder
	for _, value := range values {
		encoded := value.FillBytes(make([]byte, 32))
		output.WriteString(hex.EncodeToString(encoded))
	}
	return output.String()
}

func compositeRoundID(phase uint16, aggregatorRound uint64) *big.Int {
	value := new(big.Int).Lsh(new(big.Int).SetUint64(uint64(phase)), 64)
	return value.Or(value, new(big.Int).SetUint64(aggregatorRound))
}
