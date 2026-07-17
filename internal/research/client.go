package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxUpstreamResponseBytes = 1 << 20
	researchMaxTokens        = 8000
	maxContinuationSegments  = 3
)

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type completionRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type completionResponse struct {
	Choices []struct {
		Message      message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewClient(baseURL, apiKey, model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		baseURL: strings.TrimSpace(baseURL),
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) Research(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	if c == nil || c.baseURL == "" || c.apiKey == "" || c.model == "" {
		return "", fmt.Errorf("AI research client is not configured")
	}
	systemPrompt = strings.TrimSpace(systemPrompt)
	userMessage = strings.TrimSpace(userMessage)
	messages := make([]message, 0, 6)
	if systemPrompt != "" {
		messages = append(messages, message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, message{Role: "user", Content: userMessage})

	parts := make([]string, 0, maxContinuationSegments)
	for segment := 1; segment <= maxContinuationSegments; segment++ {
		content, finishReason, err := c.complete(ctx, messages, researchMaxTokens)
		if err != nil {
			return "", err
		}
		parts = append(parts, content)
		if !strings.EqualFold(strings.TrimSpace(finishReason), "length") {
			return joinResearchParts(parts), nil
		}
		messages = append(messages,
			message{Role: "assistant", Content: content},
			message{Role: "user", Content: "上一段因输出长度中断。请严格从中断处继续，不要重复已经完成的内容；写完剩余分析、风险提示和结论，并闭合所有Markdown标记。"},
		)
	}
	return "", fmt.Errorf("AI research response remained truncated after %d segments", maxContinuationSegments)
}

func (c *Client) complete(
	ctx context.Context,
	messages []message,
	maxTokens int,
) (string, string, error) {
	body, err := json.Marshal(completionRequest{
		Model: c.model, Messages: messages, Temperature: 0.3, MaxTokens: maxTokens,
	})
	if err != nil {
		return "", "", fmt.Errorf("encode AI research request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("create AI research request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("AI research request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamResponseBytes))
	if err != nil {
		return "", "", fmt.Errorf("read AI research response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusPaymentRequired {
			return "", "", fmt.Errorf("AI research HTTP 402 (account balance or billing unavailable)")
		}
		return "", "", fmt.Errorf("AI research HTTP %d", resp.StatusCode)
	}
	var envelope completionResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", "", fmt.Errorf("decode AI research response: %w", err)
	}
	if envelope.Error != nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return "", "", fmt.Errorf("AI research API error: %s", envelope.Error.Message)
	}
	if len(envelope.Choices) == 0 || strings.TrimSpace(envelope.Choices[0].Message.Content) == "" {
		return "", "", fmt.Errorf("AI research returned no content")
	}
	return strings.TrimSpace(envelope.Choices[0].Message.Content),
		strings.TrimSpace(envelope.Choices[0].FinishReason), nil
}

func joinResearchParts(parts []string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return strings.Join(clean, "\n")
}
