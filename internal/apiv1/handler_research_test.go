package apiv1

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubResearchProvider struct {
	content string
	err     error
	system  string
	user    string
}

func (s *stubResearchProvider) Research(_ context.Context, systemPrompt, userMessage string) (string, error) {
	s.system = systemPrompt
	s.user = userMessage
	return s.content, s.err
}

func TestGoldResearchEndpointReturnsContent(t *testing.T) {
	provider := &stubResearchProvider{content: "当前 YES 证据较强，但需注意波动风险。"}
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	srv.SetResearchProvider(provider)
	mux := http.NewServeMux()
	srv.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gold/research",
		strings.NewReader(`{"system_prompt":"系统约束","user_message":"市场上下文"}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if provider.system != "系统约束" || provider.user != "市场上下文" {
		t.Fatalf("provider received system=%q user=%q", provider.system, provider.user)
	}
	if !strings.Contains(rec.Body.String(), `"content":"当前 YES 证据较强，但需注意波动风险。"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestGoldResearchEndpointValidatesInput(t *testing.T) {
	provider := &stubResearchProvider{content: "unused"}
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	srv.SetResearchProvider(provider)
	mux := http.NewServeMux()
	srv.Register(mux)

	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{`},
		{name: "empty user message", body: `{"system_prompt":"system","user_message":""}`},
		{name: "oversized user message", body: `{"user_message":"` + strings.Repeat("x", maxResearchUserMessageBytes+1) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/gold/research", strings.NewReader(tt.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGoldResearchEndpointMapsProviderFailure(t *testing.T) {
	provider := &stubResearchProvider{err: fmt.Errorf("upstream unavailable")}
	srv := NewServer(nil, nil, nil, nil, nil, nil, nil, nil, "", 0)
	srv.SetResearchProvider(provider)
	mux := http.NewServeMux()
	srv.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/gold/research",
		strings.NewReader(`{"user_message":"市场上下文"}`)))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
