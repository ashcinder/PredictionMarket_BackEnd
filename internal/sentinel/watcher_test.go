package sentinel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/aioracle"
	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/judge"
)

func TestWinnerFromVerdict(t *testing.T) {
	tests := []struct {
		name    string
		verdict *aioracle.Verdict
		want    int
		wantErr bool
	}{
		{"resolved yes", &aioracle.Verdict{Resolved: true, Decision: aioracle.DecisionYes}, 0, false},
		{"resolved no", &aioracle.Verdict{Resolved: true, Decision: aioracle.DecisionNo}, 1, false},
		{"indeterminate", &aioracle.Verdict{Resolved: false, Decision: aioracle.DecisionIndeterminate}, -1, true},
		{"nil", nil, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := winnerFromVerdict(tt.verdict)
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Fatalf("winnerFromVerdict() = (%d, %v), want (%d, error=%v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestBuildQuantitativeAIEventIncludesRuleAndReproducibleCalculation(t *testing.T) {
	deadline := time.Date(2026, 7, 14, 7, 14, 0, 0, time.UTC)
	rule := judge.Rule{
		Type: judge.TypeRelative, Symbol: "XAU", Benchmark: "BTC", Source: "GOLD_API",
		StartTimeSec: deadline.Add(-2 * time.Minute).Unix(), EndTimeSec: deadline.Unix(),
	}
	rawRule, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	meta := &ipfs.Metadata{
		Desc: "黄金 跑赢 BTC", Condition: "黄金收益率跑赢 BTC", ResolutionRule: rawRule,
		OptionYES: "YES", OptionNO: "NO",
	}
	result := judge.Result{Determinate: true, Winner: 1, Summary: "XAU return 0.1%; BTC return 2.0%"}

	event := buildQuantitativeAIEvent(chain.GameOnChain{ID: 9, DeadlineRaw: deadline.Unix()}, meta, rule, result)
	if len(event.Evidence) != 1 {
		t.Fatalf("quantitative evidence missing: %+v", event)
	}
	content := event.Evidence[0].Content
	for _, expected := range []string{"TYPE_RELATIVE", "XAU", "BTC", "XAU return 0.1%", "BTC return 2.0%", "候选结果：NO"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("evidence missing %q: %s", expected, content)
		}
	}
}

func TestBuildAIEvent(t *testing.T) {
	deadline := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	event := buildAIEvent(chain.GameOnChain{
		ID:          42,
		DeadlineRaw: deadline.UnixMilli(),
	}, &ipfs.Metadata{
		Desc:                 "黄金价格测试",
		Condition:            "截止时 XAU/USD 高于 3000 美元",
		DetailedInfo:         "采用公开市场报价",
		OptionYES:            "高于",
		OptionNO:             "未高于",
		Keywords:             []string{"XAU/USD"},
		AuthoritativeSources: []string{"gold-api.com"},
	})

	if event.ID != "game-42" || event.Title != "黄金价格测试" {
		t.Fatalf("unexpected event identity: %+v", event)
	}
	if !event.Deadline.Equal(deadline) {
		t.Fatalf("deadline=%s, want %s", event.Deadline, deadline)
	}
	for _, expected := range []string{
		"客观判定条件", "YES 选项：高于", "NO 选项：未高于",
		"gold-api.com", "证据不足时必须降低 confidence",
	} {
		if !strings.Contains(event.Description, expected) {
			t.Errorf("description missing %q: %s", expected, event.Description)
		}
	}
	if !containsKeyword(event.Keywords, "gold") || !containsKeyword(event.Keywords, "XAU") {
		t.Fatalf("gold evidence keywords missing: %v", event.Keywords)
	}
}

func TestDeadlineTimeSupportsSecondsAndMilliseconds(t *testing.T) {
	want := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if got := deadlineTime(want.Unix()); !got.Equal(want) {
		t.Fatalf("seconds deadline=%s, want %s", got, want)
	}
	if got := deadlineTime(want.UnixMilli()); !got.Equal(want) {
		t.Fatalf("milliseconds deadline=%s, want %s", got, want)
	}
}

func TestQuantitativeMetadataWithoutResolutionRuleNeverFallsBackToNewsAI(t *testing.T) {
	for _, marketType := range []string{
		judge.TypePrice,
		judge.TypeReturnThreshold,
		judge.TypePriceThreshold,
		judge.TypePriceRange,
		judge.TypeRelative,
		judge.TypeStreak,
		judge.TypeTouch,
	} {
		if !requiresStructuredResolution(&ipfs.Metadata{Type: marketType}) {
			t.Fatalf("%s should require structured settlement evidence", marketType)
		}
	}
	if requiresStructuredResolution(&ipfs.Metadata{Type: judge.TypeEvent}) {
		t.Fatal("event market should use documentary AI evidence")
	}
}

func TestEveryVersion2RuleRequiresFinalArbiterReview(t *testing.T) {
	for _, marketType := range []string{
		judge.TypePrice, judge.TypeReturnThreshold, judge.TypePriceThreshold,
		judge.TypePriceRange, judge.TypeRelative, judge.TypeStreak,
	} {
		rule := judge.Rule{RuleVersion: 2, Type: marketType}
		if !requiresFinalArbiterReview(rule) {
			t.Fatalf("%s did not require final arbiter review", marketType)
		}
	}
	if requiresFinalArbiterReview(judge.Rule{Type: judge.TypePrice}) {
		t.Fatal("legacy deterministic rule unexpectedly requires final arbiter review")
	}
}

func TestBuildVersion2QuantitativeEventRequiresReproducibleAudit(t *testing.T) {
	deadline := time.Date(2026, 7, 15, 16, 0, 0, 0, time.UTC)
	rule := judge.Rule{
		RuleVersion: 2, Type: judge.TypePriceRange, Symbol: "XAU",
		Source: judge.ChainlinkDataFeedEthereum, SourceContract: judge.ChainlinkXAUUSDFeed,
		LowerThreshold: 4000, UpperThreshold: 4100,
		StartTimeSec: deadline.Add(-24 * time.Hour).Unix(), EndTimeSec: deadline.Unix(),
	}
	result := judge.Result{Determinate: true, Winner: 0, Summary: "round_id=42 source_time=2026-07-15T15:59:00Z price_usd=4079.105 formula=inclusive range"}
	event := buildQuantitativeAIEvent(chain.GameOnChain{ID: 10, DeadlineRaw: deadline.Unix()}, &ipfs.Metadata{
		Desc: "黄金价格 位于 4000-4100USD/盎司", Condition: "区间内为 YES",
	}, rule, result)
	content := event.Evidence[0].Content
	for _, expected := range []string{
		"TYPE_PRICE_RANGE", "round_id=42", "source_time", "price_usd=4079.105",
		"INDETERMINATE", "独立复算", "候选结果：YES",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("evidence missing %q: %s", expected, content)
		}
	}
}

func containsKeyword(keywords []string, expected string) bool {
	for _, keyword := range keywords {
		if strings.EqualFold(keyword, expected) {
			return true
		}
	}
	return false
}
