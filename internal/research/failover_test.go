package research

import (
	"context"
	"fmt"
	"testing"
)

type stubResearcher struct {
	content string
	err     error
	calls   int
}

func (s *stubResearcher) Research(_ context.Context, _, _ string) (string, error) {
	s.calls++
	return s.content, s.err
}

func TestFailoverUsesNextProviderAfterPrimaryFailure(t *testing.T) {
	primary := &stubResearcher{err: fmt.Errorf("HTTP 402 insufficient balance")}
	secondary := &stubResearcher{content: "备用模型投研结果"}
	client := NewFailover(primary, secondary)

	content, err := client.Research(context.Background(), "system", "user")
	if err != nil {
		t.Fatal(err)
	}
	if content != "备用模型投研结果" {
		t.Fatalf("content=%q", content)
	}
	if primary.calls != 1 || secondary.calls != 1 {
		t.Fatalf("calls primary=%d secondary=%d", primary.calls, secondary.calls)
	}
}

func TestFailoverReturnsErrorWhenEveryProviderFails(t *testing.T) {
	first := &stubResearcher{err: fmt.Errorf("first failed")}
	second := &stubResearcher{err: fmt.Errorf("second failed")}
	client := NewFailover(first, second)

	if _, err := client.Research(context.Background(), "system", "user"); err == nil {
		t.Fatal("expected error")
	}
}
