package sentinel

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/aioracle"
	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/chainlinkfeed"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/judge"
	"PredictionMarket/internal/marketdata"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type settlementRoundReader struct {
	rounds map[common.Address][]chainlinkfeed.Round
}

func (r *settlementRoundReader) RoundAtOrBefore(
	_ context.Context, feed common.Address, boundary time.Time,
) (chainlinkfeed.Round, error) {
	var found chainlinkfeed.Round
	for _, round := range r.rounds[feed] {
		if !round.UpdatedAt.After(boundary) &&
			(found.ID == nil || round.UpdatedAt.After(found.UpdatedAt)) {
			found = round
		}
	}
	if found.ID == nil {
		return chainlinkfeed.Round{}, sql.ErrNoRows
	}
	return found, nil
}

type settlementRoundCache struct{}

func (*settlementRoundCache) Save(context.Context, chainlinkfeed.Round) error { return nil }
func (*settlementRoundCache) LatestAtOrBefore(
	context.Context, common.Address, time.Time,
) (chainlinkfeed.Round, error) {
	return chainlinkfeed.Round{}, sql.ErrNoRows
}

type finalArbiterFixture struct {
	verdict *aioracle.Verdict
	event   *aioracle.Event
}

func (f *finalArbiterFixture) Resolve(_ context.Context, event aioracle.Event) *aioracle.Verdict {
	f.event = &event
	return f.verdict
}

type settlementFixture struct {
	name           string
	rule           judge.Rule
	rounds         map[common.Address][]chainlinkfeed.Round
	expectedWinner int
}

func TestVersion2TemplatesReachFinalArbiterAndSettlementEncoding(t *testing.T) {
	for _, fixture := range version2SettlementFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			rawTransaction := ""
			rpc := newSettlementRPC(t, &rawTransaction)
			defer rpc.Close()

			contract := common.HexToAddress("0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c")
			chainClient, err := chain.NewClient(strings.Repeat("1", 64), contract.Hex(), rpc.URL,
				"https://unused.example", false)
			if err != nil {
				t.Fatal(err)
			}
			decision := aioracle.DecisionYes
			if fixture.expectedWinner == 1 {
				decision = aioracle.DecisionNo
			}
			arbiter := &finalArbiterFixture{verdict: &aioracle.Verdict{
				Resolved: true, Decision: decision, Confidence: 0.94, ConsensusRatio: 1,
				Summary: "final arbiter reproduced the Chainlink calculation",
			}}
			watcher := NewWatcher(&config.Config{}, chainClient, ipfs.NewClient("http://unused/"), arbiter)
			watcher.SetQuantitativeResolver(marketdata.NewChainlinkResolver(
				&settlementRoundReader{rounds: fixture.rounds}, &settlementRoundCache{},
				common.HexToAddress(judge.ChainlinkXAUUSDFeed),
				common.HexToAddress(judge.ChainlinkBTCUSDFeed),
			))

			metadataCID := inlineSettlementMetadata(t, fixture.rule)
			game := chain.GameOnChain{ID: 77, IPFSCID: metadataCID, DeadlineRaw: fixture.rule.EndTimeSec}
			if err := watcher.resolveGame(context.Background(), game); err != nil {
				t.Fatal(err)
			}
			if arbiter.event == nil || len(arbiter.event.Evidence) != 1 {
				t.Fatalf("final arbiter did not receive structured evidence: %+v", arbiter.event)
			}
			evidence := arbiter.event.Evidence[0].Content
			for _, expected := range []string{
				fixture.rule.Type, "round_id=", "source_time=", "price_usd=", "independently recompute",
			} {
				if !strings.Contains(evidence, expected) {
					t.Fatalf("arbiter evidence missing %q: %s", expected, evidence)
				}
			}
			assertResolveTransaction(t, rawTransaction, 77, fixture.expectedWinner)
		})
	}
}

func TestVersion2SettlementStaysPendingWhenBoundaryEvidenceIsMissing(t *testing.T) {
	fixture := version2SettlementFixtures(t)[0]
	fixture.rounds = map[common.Address][]chainlinkfeed.Round{}
	rawTransaction := ""
	rpc := newSettlementRPC(t, &rawTransaction)
	defer rpc.Close()
	chainClient, err := chain.NewClient(strings.Repeat("1", 64),
		"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c", rpc.URL,
		"https://unused.example", false)
	if err != nil {
		t.Fatal(err)
	}
	arbiter := &finalArbiterFixture{verdict: &aioracle.Verdict{
		Resolved: true, Decision: aioracle.DecisionYes,
	}}
	watcher := NewWatcher(&config.Config{}, chainClient, ipfs.NewClient("http://unused/"), arbiter)
	watcher.SetQuantitativeResolver(marketdata.NewChainlinkResolver(
		&settlementRoundReader{rounds: fixture.rounds}, &settlementRoundCache{},
		common.HexToAddress(judge.ChainlinkXAUUSDFeed),
		common.HexToAddress(judge.ChainlinkBTCUSDFeed),
	))
	err = watcher.resolveGame(context.Background(), chain.GameOnChain{
		ID: 78, IPFSCID: inlineSettlementMetadata(t, fixture.rule),
		DeadlineRaw: fixture.rule.EndTimeSec,
	})
	if err == nil || !strings.Contains(err.Error(), "indeterminate") {
		t.Fatalf("missing evidence error=%v", err)
	}
	if arbiter.event != nil || rawTransaction != "" {
		t.Fatalf("missing evidence reached AI or chain: event=%+v tx=%q", arbiter.event, rawTransaction)
	}
}

func version2SettlementFixtures(t *testing.T) []settlementFixture {
	t.Helper()
	location, err := time.LoadLocation(judge.BeijingTimezone)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.July, 14, 0, 0, 0, 0, location)
	xau := common.HexToAddress(judge.ChainlinkXAUUSDFeed)
	btc := common.HexToAddress(judge.ChainlinkBTCUSDFeed)
	baseRule := func(ruleType string, days int) judge.Rule {
		return judge.Rule{
			RuleVersion: 2, Type: ruleType, Symbol: "XAU",
			Source: judge.ChainlinkDataFeedEthereum, SourceContract: judge.ChainlinkXAUUSDFeed,
			Timezone: judge.BeijingTimezone, BoundaryPolicy: judge.LastAtOrBefore,
			MaxStalenessSec: 43200, StartTimeSec: start.Unix(),
			EndTimeSec: start.Add(time.Duration(days) * 24 * time.Hour).Unix(),
		}
	}
	round := func(feed common.Address, id int64, boundary time.Time, price float64) chainlinkfeed.Round {
		roundID := big.NewInt(id)
		return chainlinkfeed.Round{
			Feed: feed, ID: roundID, Answer: big.NewInt(int64(price * 1e8)), Decimals: 8,
			Price: price, StartedAt: boundary.Add(-2 * time.Minute),
			UpdatedAt: boundary.Add(-time.Minute), AnsweredInRound: new(big.Int).Set(roundID),
		}
	}
	xauDay := func(prices ...float64) []chainlinkfeed.Round {
		result := make([]chainlinkfeed.Round, 0, len(prices))
		for index, price := range prices {
			result = append(result, round(xau, int64(index+1),
				start.Add(time.Duration(index)*24*time.Hour), price))
		}
		return result
	}

	price := baseRule(judge.TypePrice, 1)
	price.Direction, price.FlatTolerance = "UP", 0.05
	returnThreshold := baseRule(judge.TypeReturnThreshold, 1)
	returnThreshold.Operator, returnThreshold.Threshold = "GTE", 0.5
	priceThreshold := baseRule(judge.TypePriceThreshold, 1)
	priceThreshold.Operator, priceThreshold.Threshold = "GTE", 4050
	priceRange := baseRule(judge.TypePriceRange, 1)
	priceRange.Operator, priceRange.LowerThreshold, priceRange.UpperThreshold = "IN_RANGE", 4000, 4100
	relative := baseRule(judge.TypeRelative, 1)
	relative.Benchmark, relative.BenchmarkSourceContract = "BTC", judge.ChainlinkBTCUSDFeed
	streak := baseRule(judge.TypeStreak, 3)
	streak.Direction, streak.StreakDays = "UP", 3

	return []settlementFixture{
		{"price direction", price, map[common.Address][]chainlinkfeed.Round{xau: xauDay(4000, 4040)}, 0},
		{"absolute return threshold", returnThreshold, map[common.Address][]chainlinkfeed.Round{xau: xauDay(4000, 4040)}, 0},
		{"deadline price threshold", priceThreshold, map[common.Address][]chainlinkfeed.Round{xau: xauDay(4000, 4040)}, 1},
		{"deadline price range", priceRange, map[common.Address][]chainlinkfeed.Round{xau: xauDay(4000, 4040)}, 0},
		{"relative XAU BTC return", relative, map[common.Address][]chainlinkfeed.Round{
			xau: xauDay(4000, 4040),
			btc: {round(btc, 11, start, 100000), round(btc, 12, start.Add(24*time.Hour), 100100)},
		}, 0},
		{"three day streak", streak, map[common.Address][]chainlinkfeed.Round{xau: xauDay(4000, 4010, 4020, 4030)}, 0},
	}
}

func inlineSettlementMetadata(t *testing.T, rule judge.Rule) string {
	t.Helper()
	ruleJSON, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(ipfs.Metadata{
		Type: rule.Type, Desc: "v2 fixture " + rule.Type,
		Condition: "按已提交的 Chainlink v2 规则裁决", OptionYES: "YES", OptionNO: "NO",
		ResolutionRule: ruleJSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	return "inline-v1:" + hex.EncodeToString(body)
}

func newSettlementRPC(t *testing.T, rawTransaction *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int               `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC request: %v", err)
			return
		}
		var result any = "0x0"
		switch request.Method {
		case "eth_chainId":
			result = "0x41b"
		case "eth_getTransactionCount":
			result = "0x1"
		case "eth_gasPrice":
			result = "0x3b9aca00"
		case "eth_sendRawTransaction":
			if len(request.Params) == 1 {
				_ = json.Unmarshal(request.Params[0], rawTransaction)
			}
			result = "0xabc123"
		case "eth_getTransactionReceipt":
			result = map[string]string{"status": "0x1"}
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": result,
		})
	}))
}

func assertResolveTransaction(t *testing.T, rawTransaction string, gameID, winner int) {
	t.Helper()
	if rawTransaction == "" {
		t.Fatal("resolve transaction was not sent")
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(common.FromHex(rawTransaction)); err != nil {
		t.Fatalf("decode resolve transaction: %v", err)
	}
	want := common.FromHex(chain.EncodeResolveGame(gameID, winner))
	if !strings.EqualFold(common.Bytes2Hex(transaction.Data()), common.Bytes2Hex(want)) {
		t.Fatalf("resolve calldata=%x, want %x", transaction.Data(), want)
	}
}
