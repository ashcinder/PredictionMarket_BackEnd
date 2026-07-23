package research

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientResearchUsesConfiguredOpenAICompatibleAPI(t *testing.T) {
	var authorization, requestBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		requestBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"结构化投研结果"}}]}`))
	}))
	defer upstream.Close()

	client := NewClient(upstream.URL, "sk-test", "deepseek-chat", time.Second)
	content, err := client.Research(context.Background(), "系统提示", "市场上下文")
	if err != nil {
		t.Fatal(err)
	}
	if content != "结构化投研结果" {
		t.Fatalf("content=%q", content)
	}
	if authorization != "Bearer sk-test" {
		t.Fatalf("authorization=%q", authorization)
	}
	for _, expected := range []string{`"model":"deepseek-chat"`, `"max_tokens":8000`, `"role":"system"`, `"content":"系统提示"`, `"content":"市场上下文"`} {
		if !strings.Contains(requestBody, expected) {
			t.Fatalf("request missing %s: %s", expected, requestBody)
		}
	}
}

func TestClientResearchContinuesAndMergesLengthTruncation(t *testing.T) {
	var calls int
	var secondRequest string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"未完成的博弈池分析"},"finish_reason":"length"}]}`))
			return
		}
		secondRequest = string(body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"后半段与完整结论。"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	client := NewClient(upstream.URL, "sk-test", "deepseek-chat", time.Second)
	content, err := client.Research(context.Background(), "系统提示", "分析全部博弈池")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || content != "未完成的博弈池分析\n后半段与完整结论。" {
		t.Fatalf("calls=%d content=%q", calls, content)
	}
	for _, expected := range []string{`"max_tokens":8000`, `"role":"assistant"`, "Continue exactly where it stopped", "close all Markdown markers"} {
		if !strings.Contains(secondRequest, expected) {
			t.Fatalf("retry request missing %q: %s", expected, secondRequest)
		}
	}
}

func TestClientResearchRejectsUpstreamFailureAndEmptyChoice(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http failure", status: http.StatusBadGateway, body: `{"error":"bad gateway"}`},
		{name: "empty choice", status: http.StatusOK, body: `{"choices":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer upstream.Close()

			client := NewClient(upstream.URL, "sk-test", "deepseek-chat", time.Second)
			if _, err := client.Research(context.Background(), "system", "user"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
