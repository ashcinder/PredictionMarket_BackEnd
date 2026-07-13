package research

import (
	"context"
	"fmt"
	"strings"
)

type Researcher interface {
	Research(ctx context.Context, systemPrompt, userMessage string) (string, error)
}

type Failover struct {
	providers []Researcher
}

func NewFailover(providers ...Researcher) *Failover {
	usable := make([]Researcher, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			usable = append(usable, provider)
		}
	}
	return &Failover{providers: usable}
}

func (f *Failover) Research(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	if f == nil || len(f.providers) == 0 {
		return "", fmt.Errorf("no AI research provider is configured")
	}
	errors := make([]string, 0, len(f.providers))
	for index, provider := range f.providers {
		content, err := provider.Research(ctx, systemPrompt, userMessage)
		if err == nil && strings.TrimSpace(content) != "" {
			return strings.TrimSpace(content), nil
		}
		if err == nil {
			err = fmt.Errorf("empty response")
		}
		errors = append(errors, fmt.Sprintf("provider %d: %v", index+1, err))
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("all AI research providers failed: %s", strings.Join(errors, "; "))
}
