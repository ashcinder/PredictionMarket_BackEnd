package sentinel

import (
	"strings"
	"testing"
	"time"

	"PredictionMarket/internal/aioracle"
	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/ipfs"
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

func containsKeyword(keywords []string, expected string) bool {
	for _, keyword := range keywords {
		if strings.EqualFold(keyword, expected) {
			return true
		}
	}
	return false
}
