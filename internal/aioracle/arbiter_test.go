package aioracle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type deliberationTestProvider struct {
	name       string
	opinion    ModelOpinion
	final      *FinalJudgment
	finalErr   error
	mu         sync.Mutex
	received   []ModelOpinion
	articles   []NewsArticle
	finalCalls int
}

func (p *deliberationTestProvider) Name() string    { return p.name }
func (p *deliberationTestProvider) ModelID() string { return p.name + "-model" }
func (p *deliberationTestProvider) Weight() float64 { return 1 }

func (p *deliberationTestProvider) Query(_ context.Context, _ Event, articles []NewsArticle) (*ModelOpinion, error) {
	p.mu.Lock()
	p.articles = append([]NewsArticle(nil), articles...)
	p.mu.Unlock()
	opinion := p.opinion
	opinion.ModelName = p.name
	return &opinion, nil
}
func (p *deliberationTestProvider) QueryFinal(_ context.Context, _ Event, articles []NewsArticle, opinions []ModelOpinion) (*FinalJudgment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finalCalls++
	p.articles = append([]NewsArticle(nil), articles...)
	p.received = append([]ModelOpinion(nil), opinions...)
	return p.final, p.finalErr
}

func TestOracleDeliversEmbeddedQuantitativeEvidenceToPeersAndFinalArbiter(t *testing.T) {
	peer := &deliberationTestProvider{name: "peer", opinion: ModelOpinion{
		Occurred: false, Decision: DecisionNo, Confidence: 0.99, Reasoning: "BTC 收益率更高",
	}}
	arbiter := &deliberationTestProvider{name: "arbiter", final: &FinalJudgment{
		Decision: DecisionNo, Confidence: 0.99, Reasoning: "复算收益率后裁定 NO",
	}}
	engine := NewConsensusEngine(ConsensusConfig{FinalArbiter: "arbiter"}, []ModelProvider{peer, arbiter})
	oracle := NewOracleWithOptions(nil, engine, OracleOptions{})
	evidence := NewsArticle{
		Title: "XAU/BTC 两分钟收益率证据", Source: "GOLD_API + COINBASE_EXCHANGE",
		Content: "XAU return 0.1%; BTC return 2.0%",
	}

	verdict := oracle.Resolve(context.Background(), Event{
		ID: "game-relative", Title: "黄金 跑赢 BTC", Deadline: time.Now(),
		Evidence: []NewsArticle{evidence},
	})
	if !verdict.Resolved || verdict.Decision != DecisionNo {
		t.Fatalf("unexpected verdict: %+v", verdict)
	}
	for _, provider := range []*deliberationTestProvider{peer, arbiter} {
		if len(provider.articles) != 1 || provider.articles[0].Content != evidence.Content {
			t.Fatalf("%s did not receive embedded evidence: %+v", provider.name, provider.articles)
		}
	}
}

func TestFinalArbiterReceivesPeerOpinionsAndControlsVerdict(t *testing.T) {
	peerYes := &deliberationTestProvider{name: "peer-yes", opinion: ModelOpinion{Occurred: true, Confidence: 0.91, Reasoning: "证据支持 YES"}}
	peerNo := &deliberationTestProvider{name: "peer-no", opinion: ModelOpinion{Occurred: false, Confidence: 0.88, Reasoning: "证据支持 NO"}}
	arbiter := &deliberationTestProvider{name: "arbiter", final: &FinalJudgment{
		Decision: DecisionYes, Confidence: 0.55, Reasoning: "综合证据与两份独立意见后裁定 YES",
	}}
	engine := NewConsensusEngine(ConsensusConfig{
		FinalArbiter:      "arbiter",
		MinConsensusRatio: 1,
		MinConfidence:     1,
		MinModelsRequired: 3,
	}, []ModelProvider{peerYes, peerNo, arbiter})

	verdict := engine.Judge(context.Background(), Event{ID: "game-1", Title: "测试事件"}, nil)

	if !verdict.Resolved || verdict.Decision != DecisionYes || !verdict.Occurred {
		t.Fatalf("final arbiter did not control verdict: %+v", verdict)
	}
	if verdict.Confidence != 0.55 {
		t.Fatalf("expected arbiter confidence without backend threshold, got %.2f", verdict.Confidence)
	}
	if arbiter.finalCalls != 1 || len(arbiter.received) != 2 {
		t.Fatalf("arbiter did not receive N-1 opinions: calls=%d opinions=%+v", arbiter.finalCalls, arbiter.received)
	}
	if len(verdict.Opinions) != 3 || !verdict.Opinions[2].IsFinal {
		t.Fatalf("final opinion missing from audit trail: %+v", verdict.Opinions)
	}
	if !strings.Contains(verdict.Summary, "arbiter") {
		t.Fatalf("summary does not identify final arbiter: %s", verdict.Summary)
	}
}

func TestFinalArbiterFailureLeavesVerdictIndeterminate(t *testing.T) {
	peer := &deliberationTestProvider{name: "peer", opinion: ModelOpinion{Occurred: true, Confidence: 0.9}}
	arbiter := &deliberationTestProvider{name: "arbiter", finalErr: errors.New("upstream unavailable")}
	engine := NewConsensusEngine(ConsensusConfig{FinalArbiter: "arbiter"}, []ModelProvider{peer, arbiter})

	verdict := engine.Judge(context.Background(), Event{ID: "game-2"}, nil)

	if verdict.Resolved || verdict.Decision != DecisionIndeterminate {
		t.Fatalf("arbiter failure must not settle market: %+v", verdict)
	}
	if !strings.Contains(verdict.Summary, "final arbiter") {
		t.Fatalf("missing arbiter failure summary: %s", verdict.Summary)
	}
}

func TestFinalArbiterCanExplicitlyAbstainWithoutFalseConsensus(t *testing.T) {
	peer := &deliberationTestProvider{name: "peer", opinion: ModelOpinion{Occurred: true, Confidence: 0.7}}
	arbiter := &deliberationTestProvider{name: "arbiter", final: &FinalJudgment{
		Decision: DecisionIndeterminate, Confidence: 0.4, Reasoning: "证据互相冲突，暂不裁定",
	}}
	engine := NewConsensusEngine(ConsensusConfig{FinalArbiter: "arbiter"}, []ModelProvider{peer, arbiter})

	verdict := engine.Judge(context.Background(), Event{ID: "game-3"}, nil)

	if verdict.Resolved || verdict.Decision != DecisionIndeterminate {
		t.Fatalf("explicit abstention must remain unresolved: %+v", verdict)
	}
	if verdict.AgreeingModels != 0 || verdict.ConsensusRatio != 0 {
		t.Fatalf("abstention must not report false consensus: %+v", verdict)
	}
}

func TestSummarizeOpinionsIncludesEveryPeerResult(t *testing.T) {
	opinions := []ModelOpinion{
		{
			ModelName: "glm", ModelID: "glm-4", Decision: DecisionYes,
			Confidence: 0.88, Reasoning: "官方公告确认事件发生",
		},
		{
			ModelName: "deepseek", ModelID: "deepseek-chat",
			Decision: DecisionIndeterminate, Error: "HTTP 402",
		},
	}

	summary := summarizeOpinions(opinions)
	for _, expected := range []string{"glm/glm-4", "YES", "0.88", "官方公告确认事件发生", "deepseek/deepseek-chat", "HTTP 402"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("opinion summary missing %q: %s", expected, summary)
		}
	}
}

func TestSummarizeEvidenceIncludesAuditableArticleContext(t *testing.T) {
	publishedAt := time.Date(2026, 7, 14, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	articles := []NewsArticle{{
		Source: "Federal Reserve", Title: "FOMC statement", URL: "https://example.com/fomc",
		PublishedAt: publishedAt, Content: "The committee decided to lower the target range.",
	}}

	summary := summarizeEvidence(articles)
	for _, expected := range []string{"Federal Reserve", "FOMC statement", "2026-07-14 09:30", "lower the target range"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("evidence summary missing %q: %s", expected, summary)
		}
	}
}

func TestStructuredEvidenceIsNotCutBeforePriceInModelPrompts(t *testing.T) {
	content := strings.Repeat("规则扩展字段", 90) +
		" feed=0x214e boundary=2026-07-15T16:00:00Z" +
		" round_id=92233720368547766377 source_time=2026-07-15T15:59:00Z price_usd=4079.105" +
		"\n行情计算：deadline close 4079.10500000 compared with threshold 4000.00000000" +
		"\n确定性候选结果：YES"
	article := NewsArticle{
		Title:   "结构化行情计算证据：黄金价格大于等于4000美元/盎司",
		Source:  "CHAINLINK_DATA_FEED_ETHEREUM",
		Content: content,
	}
	event := Event{ID: "game-2", Title: "黄金价格阈值", Deadline: time.Now()}

	peerPrompt := buildOraclePrompt(event, []NewsArticle{article})
	finalPrompt := buildFinalArbiterPrompt(event, []NewsArticle{article}, nil)
	for name, prompt := range map[string]string{"peer": peerPrompt, "final": finalPrompt} {
		for _, expected := range []string{
			"round_id=92233720368547766377",
			"source_time=2026-07-15T15:59:00Z",
			"price_usd=4079.105",
			"确定性候选结果：YES",
		} {
			if !strings.Contains(prompt, expected) {
				t.Fatalf("%s prompt truncated structured evidence before %q", name, expected)
			}
		}
	}
}

func TestOpenAICompatibleFinalRetriesTruncatedJSON(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request openAICompatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.MaxTokens != 2400 {
			t.Errorf("final max_tokens=%d, want 2400", request.MaxTokens)
		}
		content := `{"decision":"YES","confidence":1,"reasoning":"未闭合`
		if calls == 2 {
			if !strings.Contains(request.Messages[1].Content, "previous JSON was incomplete") {
				t.Error("retry prompt does not request concise, complete JSON")
			}
			content = `{"decision":"YES","confidence":1,"reasoning":"复算通过","sources":[]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]string{"content": content},
			}},
		})
	}))
	defer server.Close()

	provider, err := newOpenAIProvider(ProviderConfig{
		Name: "minimax", Model: "test-model", APIKey: "test-key",
		BaseURL: server.URL, Provider: "minimax", TimeoutSeconds: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	judgment, err := provider.(FinalModelProvider).QueryFinal(
		context.Background(), Event{ID: "game-2", Title: "价格阈值"}, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || judgment.Decision != DecisionYes {
		t.Fatalf("calls=%d judgment=%+v", calls, judgment)
	}
}
