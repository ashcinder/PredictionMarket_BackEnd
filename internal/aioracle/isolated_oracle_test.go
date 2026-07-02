package aioracle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestThreeProviderOracleWithoutDatabaseOrChain is the hermetic end-to-end
// oracle test. It uses only in-memory HTTP servers: no MySQL connection, chain
// client, RPC call, wallet, or transaction is created.
func TestThreeProviderOracleWithoutDatabaseOrChain(t *testing.T) {
	var deepSeekCalls, glmCalls, miniMaxCalls atomic.Int32
	deepSeek := newOracleProviderServer(t, &deepSeekCalls,
		`{"occurred":true,"confidence":0.91,"reasoning":"测试证据确认","sources":["https://example.invalid/evidence"]}`)
	defer deepSeek.Close()
	glm := newOracleProviderServer(t, &glmCalls,
		`{"occurred":true,"confidence":0.92,"reasoning":"测试证据确认","sources":["https://example.invalid/evidence"]}`)
	defer glm.Close()
	miniMax := newOracleProviderServer(t, &miniMaxCalls,
		`<think>内部推理不应进入 JSON 解析。</think>{"occurred":true,"confidence":0.93,"reasoning":"测试证据确认","sources":["https://example.invalid/evidence"]}`)
	defer miniMax.Close()

	providers := NewProviders([]ProviderConfig{
		{
			Name: "deepseek", Model: "deepseek-chat", APIKey: "test-key",
			BaseURL: deepSeek.URL, Provider: "deepseek", Weight: 1, TimeoutSeconds: 2,
		},
		{
			Name: "glm", Model: "glm-test", APIKey: "test-key",
			BaseURL: glm.URL, Provider: "glm", Weight: 1, TimeoutSeconds: 2,
		},
		{
			Name: "minimax", Model: "MiniMax-test", APIKey: "test-key",
			BaseURL: miniMax.URL, Provider: "minimax", Weight: 1, TimeoutSeconds: 2,
		},
	})
	if len(providers) != 3 {
		t.Fatalf("initialized %d providers, want 3", len(providers))
	}

	engine := NewConsensusEngine(ConsensusConfig{
		MinConsensusRatio: 0.66,
		MinConfidence:     0.60,
		MinModelsRequired: 3,
	}, providers)
	oracle := NewOracle(fixedIsolatedEvidence{}, engine, time.Minute)
	verdict := oracle.Resolve(context.Background(), Event{
		ID:          "isolated-three-provider-test",
		Title:       "隔离测试事件",
		Description: "当测试证据中包含 ORACLE_TEST_OK 时，事件成立。",
		Keywords:    []string{"ORACLE_TEST_OK"},
		Deadline:    time.Now().Add(time.Hour),
	})

	if !verdict.Resolved || verdict.Decision != DecisionYes {
		t.Fatalf("unexpected verdict: resolved=%v decision=%s summary=%s",
			verdict.Resolved, verdict.Decision, verdict.Summary)
	}
	if verdict.TotalModels != 3 || verdict.AgreeingModels != 3 || verdict.ConsensusRatio != 1 {
		t.Fatalf("unexpected consensus: %+v", verdict)
	}
	if deepSeekCalls.Load() != 1 || glmCalls.Load() != 1 || miniMaxCalls.Load() != 1 {
		t.Fatalf("provider calls: deepseek=%d glm=%d minimax=%d",
			deepSeekCalls.Load(), glmCalls.Load(), miniMaxCalls.Load())
	}
	for _, opinion := range verdict.Opinions {
		if opinion.Error != "" {
			t.Fatalf("%s returned error: %s", opinion.ModelName, opinion.Error)
		}
	}
}

type fixedIsolatedEvidence struct{}

func (fixedIsolatedEvidence) Fetch(_ context.Context, _ []string, _ time.Time, _ int) ([]NewsArticle, error) {
	return []NewsArticle{{
		Title:       "Synthetic oracle evidence",
		URL:         "https://example.invalid/evidence",
		Source:      "In-memory test fixture",
		PublishedAt: time.Now(),
		Content:     "ORACLE_TEST_OK",
	}}, nil
}

func newOracleProviderServer(t *testing.T, calls *atomic.Int32, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method=%s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing bearer authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": content}},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}
