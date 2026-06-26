package aioracle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildOraclePrompt(t *testing.T) {
	deadline := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	event := Event{
		ID:          "btc-150k",
		Title:       "BTC exceeds $150,000",
		Description: "Bitcoin price exceeds $150,000 USD on any major exchange before July 1, 2026.",
		Keywords:    []string{"bitcoin", "BTC", "150000"},
		Deadline:    deadline,
	}

	articles := []NewsArticle{
		{
			Title:       "Bitcoin hits new all-time high of $155,000",
			URL:         "https://example.com/btc-ath",
			Source:      "CryptoNews",
			PublishedAt: time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
			Content:     "Bitcoin reached a new record of $155,000 on Tuesday...",
		},
	}

	prompt := buildOraclePrompt(event, articles)

	checks := []string{
		"btc-150k",
		"BTC exceeds $150,000",
		"Bitcoin price exceeds $150,000",
		"bitcoin、BTC、150000",
		"July 1, 2026",
		"CryptoNews",
		"Bitcoin hits new all-time high",
		"https://example.com/btc-ath",
		"$155,000",
		"occurred",
		"confidence",
		"reasoning",
		"sources",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing expected string %q", check)
		}
	}

	// Verify system instructions are not injected via news content.
	if strings.Contains(prompt, "忽略之前") {
		t.Error("prompt should not contain injection markers")
	}
}

func TestBuildOraclePrompt_NoArticles(t *testing.T) {
	event := Event{
		ID:          "test",
		Title:       "Test Event",
		Description: "A test event with no articles.",
		Deadline:    time.Now(),
	}
	prompt := buildOraclePrompt(event, nil)
	if !strings.Contains(prompt, "无可用新闻源") {
		t.Error("prompt should mention no news available")
	}
}

func TestParseOracleResponse_ValidJSON(t *testing.T) {
	content := `{"occurred": true, "confidence": 0.95, "reasoning": "多个权威新闻源确认", "sources": ["https://example.com/1"]}`
	opinion, err := parseOracleResponse("test-model", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opinion.Occurred {
		t.Error("expected occurred=true")
	}
	if opinion.Confidence != 0.95 {
		t.Errorf("expected confidence=0.95, got %.2f", opinion.Confidence)
	}
	if len(opinion.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(opinion.Sources))
	}
}

func TestParseOracleResponse_MarkdownFenced(t *testing.T) {
	content := "```json\n{\"occurred\": false, \"confidence\": 0.10, \"reasoning\": \"无证据\", \"sources\": []}\n```"
	opinion, err := parseOracleResponse("test-model", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opinion.Occurred {
		t.Error("expected occurred=false")
	}
	if opinion.Confidence != 0.10 {
		t.Errorf("expected confidence=0.10, got %.2f", opinion.Confidence)
	}
}

func TestParseOracleResponse_ExtraText(t *testing.T) {
	content := `Here is my analysis: {"occurred": true, "confidence": 0.88, "reasoning": "明确证据", "sources": ["a.com"]} Hope this helps.`
	opinion, err := parseOracleResponse("test-model", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opinion.Occurred {
		t.Error("expected occurred=true")
	}
}

func TestParseOracleResponse_ClampsConfidence(t *testing.T) {
	// Confidence out of range should be clamped.
	content := `{"occurred": true, "confidence": 1.5, "reasoning": "test", "sources": []}`
	opinion, err := parseOracleResponse("test-model", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opinion.Confidence != 1.0 {
		t.Errorf("expected confidence clamped to 1.0, got %.2f", opinion.Confidence)
	}

	content = `{"occurred": false, "confidence": -0.5, "reasoning": "test", "sources": []}`
	opinion, err = parseOracleResponse("test-model", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opinion.Confidence != 0 {
		t.Errorf("expected confidence clamped to 0, got %.2f", opinion.Confidence)
	}
}

func TestParseOracleResponse_InvalidJSON(t *testing.T) {
	content := `not json at all`
	opinion, err := parseOracleResponse("test-model", content)
	if err != nil {
		t.Fatalf("parseOracleResponse should not return error for invalid JSON (sets Error field): %v", err)
	}
	if opinion.Error == "" {
		t.Error("expected Error field to be set for invalid JSON")
	}
}

// =============================================================================
// Provider integration tests (using httptest)
// =============================================================================

func TestDeepSeekProvider_Query(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type header")
		}

		resp := openAICompatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"occurred": true, "confidence": 0.92, "reasoning": "新闻确认BTC超过$150k", "sources": ["https://example.com/btc"]}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &deepSeekProvider{
		name:    "test-deepseek",
		model:   "deepseek-chat",
		apiKey:  "sk-test",
		baseURL: server.URL,
		weight:  1.0,
		client:  server.Client(),
	}

	event := Event{
		ID:          "test",
		Title:       "Test",
		Description: "Test event",
		Deadline:    time.Now().Add(time.Hour),
	}

	opinion, err := p.Query(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if !opinion.Occurred {
		t.Error("expected occurred=true")
	}
	if opinion.Confidence != 0.92 {
		t.Errorf("expected confidence=0.92, got %.2f", opinion.Confidence)
	}
	if opinion.ModelName != "deepseek-chat" {
		t.Errorf("expected model name deepseek-chat, got %s", opinion.ModelName)
	}
}

func TestAnthropicProvider_Query(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing or wrong anthropic-version header")
		}

		resp := anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: `{"occurred": false, "confidence": 0.30, "reasoning": "证据不足", "sources": []}`},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &anthropicProvider{
		name:    "test-claude",
		model:   "claude-sonnet-4-20250514",
		apiKey:  "sk-ant-test",
		baseURL: server.URL,
		weight:  1.2,
		client:  server.Client(),
	}

	event := Event{
		ID:          "test",
		Title:       "Test",
		Description: "Test event",
		Deadline:    time.Now().Add(time.Hour),
	}

	opinion, err := p.Query(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if opinion.Occurred {
		t.Error("expected occurred=false")
	}
	if opinion.Confidence != 0.30 {
		t.Errorf("expected confidence=0.30, got %.2f", opinion.Confidence)
	}
}

func TestOpenAIProvider_Query(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAICompatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: `{"occurred": true, "confidence": 0.75, "reasoning": "部分确认", "sources": ["https://x.com"]}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &openAIProvider{
		name:    "test-openai",
		model:   "gpt-4o",
		apiKey:  "sk-test",
		baseURL: server.URL,
		weight:  1.0,
		client:  server.Client(),
	}

	event := Event{
		ID:          "test",
		Title:       "Test",
		Description: "Test event",
		Deadline:    time.Now().Add(time.Hour),
	}

	opinion, err := p.Query(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if opinion.Confidence != 0.75 {
		t.Errorf("expected confidence=0.75, got %.2f", opinion.Confidence)
	}
}

func TestProviderFactory(t *testing.T) {
	tests := []struct {
		providerType string
		expectErr    bool
	}{
		{"deepseek", false},
		{"openai", false},
		{"anthropic", false},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.providerType, func(t *testing.T) {
			cfg := ProviderConfig{
				Name:     "test",
				Model:    "test-model",
				APIKey:   "sk-test",
				BaseURL:  "https://example.com",
				Provider: tt.providerType,
				Weight:   1.0,
			}
			_, err := NewProvider(cfg)
			if (err != nil) != tt.expectErr {
				t.Errorf("NewProvider(%q) error=%v, wantErr=%v", tt.providerType, err, tt.expectErr)
			}
		})
	}
}

// =============================================================================
// Consensus engine tests
// =============================================================================

func TestConsensusEngine_UnanimousYes(t *testing.T) {
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.9}},
		&mockProvider{name: "m2", model: "claude", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.85}},
		&mockProvider{name: "m3", model: "deepseek", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.88}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.66,
		MinConfidence:     0.60,
		MinModelsRequired: 2,
	}
	engine := NewConsensusEngine(cfg, providers)

	event := Event{ID: "e1", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	if !verdict.Occurred {
		t.Error("expected unanimous YES verdict")
	}
	if verdict.ConsensusRatio < 0.99 {
		t.Errorf("expected consensus ratio near 1.0, got %.2f", verdict.ConsensusRatio)
	}
	if verdict.AgreeingModels != 3 {
		t.Errorf("expected 3 agreeing models, got %d", verdict.AgreeingModels)
	}
	if verdict.Confidence < 0.85 {
		t.Errorf("expected high confidence, got %.2f", verdict.Confidence)
	}
}

func TestConsensusEngine_MajorityNo(t *testing.T) {
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4", weight: 1.0, opinion: &ModelOpinion{Occurred: false, Confidence: 0.9}},
		&mockProvider{name: "m2", model: "claude", weight: 1.0, opinion: &ModelOpinion{Occurred: false, Confidence: 0.8}},
		&mockProvider{name: "m3", model: "deepseek", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.6}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.50,
		MinConfidence:     0.60,
		MinModelsRequired: 2,
	}
	engine := NewConsensusEngine(cfg, providers)

	event := Event{ID: "e2", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	if verdict.Occurred {
		t.Error("expected NO verdict (majority says not occurred)")
	}
	if verdict.AgreeingModels != 2 {
		t.Errorf("expected 2 agreeing models, got %d", verdict.AgreeingModels)
	}
}

func TestConsensusEngine_BelowMinRatio(t *testing.T) {
	// Evenly split vote with high threshold should fail.
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.9}},
		&mockProvider{name: "m2", model: "claude", weight: 1.0, opinion: &ModelOpinion{Occurred: false, Confidence: 0.9}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.90, // Need 90% consensus, but only 50% exists
		MinConfidence:     0.50,
		MinModelsRequired: 2,
	}
	engine := NewConsensusEngine(cfg, providers)

	event := Event{ID: "e3", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	if verdict.Occurred {
		t.Error("expected verdict NOT occurred due to insufficient consensus ratio")
	}
	if !strings.Contains(verdict.Summary, "consensus ratio") {
		t.Errorf("summary should mention consensus ratio: %s", verdict.Summary)
	}
}

func TestConsensusEngine_BelowMinConfidence(t *testing.T) {
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.3}},
		&mockProvider{name: "m2", model: "claude", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.3}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.50,
		MinConfidence:     0.80, // Models agree but with low confidence individually
		MinModelsRequired: 2,
	}
	engine := NewConsensusEngine(cfg, providers)

	event := Event{ID: "e4", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	if verdict.Occurred {
		t.Error("expected verdict NOT occurred due to low aggregated confidence")
	}
}

func TestConsensusEngine_InsufficientModels(t *testing.T) {
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4", weight: 1.0, err: true},
		&mockProvider{name: "m2", model: "claude", weight: 1.0, err: true},
		&mockProvider{name: "m3", model: "deepseek", weight: 1.0, err: true},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.66,
		MinConfidence:     0.60,
		MinModelsRequired: 3,
	}
	engine := NewConsensusEngine(cfg, providers)

	event := Event{ID: "e5", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	// All three opinions are recorded (with Error set), but 0 are valid (no Error).
	validCount := 0
	for _, op := range verdict.Opinions {
		if op.Error == "" {
			validCount++
		}
	}
	if validCount != 0 {
		t.Errorf("expected 0 valid opinions (all errored), got %d valid", validCount)
	}
	if len(verdict.Opinions) != 3 {
		t.Errorf("expected 3 total opinions recorded (for audit trail), got %d", len(verdict.Opinions))
	}
	if !strings.Contains(verdict.Summary, "insufficient data") {
		t.Errorf("summary should mention insufficient data: %s", verdict.Summary)
	}
}

func TestConsensusEngine_Tiebreak(t *testing.T) {
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4o", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.8}},
		&mockProvider{name: "m2", model: "claude", weight: 1.0, opinion: &ModelOpinion{Occurred: false, Confidence: 0.8}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.50,
		MinConfidence:     0.50,
		MinModelsRequired: 2,
		TiebreakModel:     "m2", // claude breaks the tie
	}
	engine := NewConsensusEngine(cfg, providers)

	event := Event{ID: "e6", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	// With equal weights and opposite votes, tiebreak should be used.
	if verdict.Occurred {
		t.Error("expected tiebreak to pick NO (claude's vote)")
	}
	if !strings.Contains(verdict.Summary, "tie broken") {
		t.Errorf("summary should mention tiebreak: %s", verdict.Summary)
	}
}

func TestConsensusEngine_WeightedVoting(t *testing.T) {
	// m2 has 3x the weight of m1 — its opinion should dominate.
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.95}},
		&mockProvider{name: "m2", model: "claude", weight: 3.0, opinion: &ModelOpinion{Occurred: false, Confidence: 0.90}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.50,
		MinConfidence:     0.50,
		MinModelsRequired: 2,
	}
	engine := NewConsensusEngine(cfg, providers)

	event := Event{ID: "e7", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	// m2 has 3x weight, so NO should win despite only 1 model voting NO.
	if verdict.Occurred {
		t.Errorf("expected NO (m2 has 3x weight), got YES. ConsensusRatio=%.2f", verdict.ConsensusRatio)
	}
}

func TestConsensusEngine_ZeroProviders(t *testing.T) {
	cfg := ConsensusConfig{
		MinConsensusRatio: 0.66,
		MinConfidence:     0.60,
		MinModelsRequired: 1,
	}
	engine := NewConsensusEngine(cfg, nil)

	event := Event{ID: "e8", Title: "Test", Description: "Test", Deadline: time.Now().Add(time.Hour)}
	verdict := engine.Judge(context.Background(), event, nil)

	if verdict.Occurred {
		t.Error("expected no verdict with zero providers")
	}
}

// =============================================================================
// Mock provider for consensus tests
// =============================================================================

type mockProvider struct {
	name    string
	model   string
	weight  float64
	opinion *ModelOpinion
	err     bool
}

func (m *mockProvider) Name() string    { return m.name }
func (m *mockProvider) ModelID() string  { return m.model }
func (m *mockProvider) Weight() float64  { return m.weight }

func (m *mockProvider) Query(ctx context.Context, event Event, articles []NewsArticle) (*ModelOpinion, error) {
	if m.err {
		return &ModelOpinion{ModelName: m.name, Error: "mock failure"}, nil
	}
	op := *m.opinion
	op.ModelName = m.name
	return &op, nil
}

// =============================================================================
// Oracle integration tests
// =============================================================================

func TestOracle_Resolve(t *testing.T) {
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "gpt-4", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.9}},
		&mockProvider{name: "m2", model: "claude", weight: 1.0, opinion: &ModelOpinion{Occurred: true, Confidence: 0.85}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.66,
		MinConfidence:     0.60,
		MinModelsRequired: 2,
	}
	engine := NewConsensusEngine(cfg, providers)
	oracle := NewOracle(nil, engine, time.Minute)

	event := Event{
		ID:          "test-event",
		Title:       "Test Event",
		Description: "A test event for integration testing.",
		Keywords:    []string{"test"},
		Deadline:    time.Now().Add(time.Hour),
	}

	verdict := oracle.Resolve(context.Background(), event)

	if !verdict.Occurred {
		t.Error("expected verdict occurred=true")
	}
	if verdict.EventID != "test-event" {
		t.Errorf("expected event ID test-event, got %s", verdict.EventID)
	}

	// Verify it was stored.
	cached := oracle.LatestVerdict("test-event")
	if cached == nil {
		t.Error("verdict should be cached after Resolve")
	}
}

func TestSimpleResolve(t *testing.T) {
	providers := []ModelProvider{
		&mockProvider{name: "m1", model: "test", weight: 1.0, opinion: &ModelOpinion{Occurred: false, Confidence: 0.7}},
	}

	cfg := ConsensusConfig{
		MinConsensusRatio: 0.50,
		MinConfidence:     0.50,
		MinModelsRequired: 1,
	}
	engine := NewConsensusEngine(cfg, providers)
	oracle := NewOracle(nil, engine, time.Minute)

	verdict := SimpleResolve(context.Background(), oracle,
		"event-1", "Test", "Test description",
		[]string{"keyword"}, time.Now().Add(time.Hour),
	)

	if verdict.EventID != "event-1" {
		t.Errorf("expected event-1, got %s", verdict.EventID)
	}
}

// =============================================================================
// News helper tests
// =============================================================================

func TestParseRSSDate(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Time
	}{
		{"Mon, 02 Jan 2006 15:04:05 MST", time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)},
		{"2024-06-15T12:00:00Z", time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)},
		{"", time.Time{}},
		{"not a date", time.Time{}},
	}

	for _, tt := range tests {
		result := parseRSSDate(tt.input)
		if tt.expected.IsZero() && !result.IsZero() {
			t.Errorf("parseRSSDate(%q) = %v, expected zero time", tt.input, result)
		} else if !tt.expected.IsZero() && !result.Equal(tt.expected) {
			t.Errorf("parseRSSDate(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestMatchesKeywords(t *testing.T) {
	article := NewsArticle{
		Title:   "Bitcoin hits $155,000 all-time high",
		Content: "The cryptocurrency market surged today.",
	}

	if !matchesKeywords(article, []string{"bitcoin"}) {
		t.Error("should match 'bitcoin' in title")
	}
	if !matchesKeywords(article, []string{"cryptocurrency"}) {
		t.Error("should match 'cryptocurrency' in content")
	}
	if matchesKeywords(article, []string{"ethereum"}) {
		t.Error("should not match 'ethereum'")
	}
	if !matchesKeywords(article, nil) {
		t.Error("empty keywords should match everything")
	}
}

func TestTruncateContent(t *testing.T) {
	short := "hello"
	if truncateContent(short, 100) != short {
		t.Error("short content should not be truncated")
	}

	long := "这是一个很长的中文字符串用于测试截断功能是否正常工作"
	truncated := truncateContent(long, 10)
	if len([]rune(truncated)) > 10+3 { // +3 for "..."
		t.Errorf("truncated too long: %s (%d runes)", truncated, len([]rune(truncated)))
	}
	if !strings.HasSuffix(truncated, "...") {
		t.Error("truncated content should end with '...'")
	}
}

func TestNewProviders_SkipsInvalid(t *testing.T) {
	configs := []ProviderConfig{
		{Name: "valid", Model: "deepseek-chat", APIKey: "sk-test", Provider: "deepseek", Weight: 1.0},
		{Name: "invalid", Model: "", APIKey: "", Provider: "unknown", Weight: 1.0},
	}

	providers := NewProviders(configs)
	if len(providers) != 1 {
		t.Errorf("expected 1 valid provider, got %d", len(providers))
	}
	if providers[0].Name() != "valid" {
		t.Errorf("expected 'valid' provider, got %s", providers[0].Name())
	}
}
