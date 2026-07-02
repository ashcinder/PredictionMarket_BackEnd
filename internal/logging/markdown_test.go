package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMarkdownRouterRoutesAndRedacts(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	router, err := NewMarkdownRouter(slog.NewTextHandler(&console, nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(router)
	logger.Info("sentinel round started", "round", 1)
	logger.Info("prediction market sentinel started", "contract", "0x123")
	logger.Info("sentinel round completed", "round", 1, "total_games", 2)
	logger.Info("sentinel round started", "round", 2)
	logger.Warn("ai-managed trade executed", "private_key", "should-not-appear", "note", "a|b\nc")
	logger.Info("sampler: cycle complete", "games", 3, "api_key", "sk-abcdefghijklmnop")
	logger.Info("unclassified general log", "value", 1)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	chain := readLog(t, dir, chainPoolFile)
	managed := readLog(t, dir, aiManagedFile)
	poll := readLog(t, dir, chainPollFile)
	if !strings.Contains(chain, "prediction market sentinel started") {
		t.Fatalf("chain log missing routed message:\n%s", chain)
	}
	if !strings.Contains(chain, "### 🔄 第 1 轮") ||
		!strings.Contains(chain, "### 🔄 第 2 轮") ||
		!strings.Contains(chain, "## 🚀 后端会话") ||
		strings.Contains(chain, "| 时间 | 级别 | 消息 | 详情 |") {
		t.Fatalf("round sections missing or malformed:\n%s", chain)
	}
	if !strings.Contains(managed, "ai-managed trade executed") ||
		!strings.Contains(managed, "**private_key**：[REDACTED]") ||
		strings.Contains(managed, "should-not-appear") {
		t.Fatalf("managed log routing/redaction failed:\n%s", managed)
	}
	if !strings.Contains(managed, `a\|b<br>c`) {
		t.Fatalf("markdown escaping failed:\n%s", managed)
	}
	if !strings.Contains(poll, "sampler: cycle complete") ||
		strings.Contains(poll, "sk-abcdefghijklmnop") {
		t.Fatalf("poll log routing/redaction failed:\n%s", poll)
	}
	for _, body := range []string{chain, managed, poll} {
		if strings.Contains(body, "unclassified general log") {
			t.Fatalf("unclassified message should not be written to category logs:\n%s", body)
		}
	}
	if !strings.Contains(console.String(), "unclassified general log") {
		t.Fatal("console handler did not receive all logs")
	}
}

func TestConciseConsoleHandler(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewConciseConsoleHandler(&output))
	logger.Info("sampler: verbose detail", "game_id", 1)
	logger.Info("sentinel round started", "round", 1)
	logger.Info("sentinel round completed", "round", 1, "total_games", 3)
	logger.Info("game resolved on chain", "game_id", 2)
	logger.Warn("temporary provider failure", "provider", "glm")

	text := output.String()
	for _, hidden := range []string{"sampler: verbose detail", "sentinel round started"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("verbose terminal message was not filtered: %s", text)
		}
	}
	for _, visible := range []string{"sentinel round completed", "game resolved on chain", "temporary provider failure"} {
		if !strings.Contains(text, visible) {
			t.Fatalf("essential terminal message %q missing: %s", visible, text)
		}
	}
}

func TestMarkdownRouterConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	router, err := NewMarkdownRouter(slog.NewTextHandler(&bytes.Buffer{}, nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(router)
	const count = 50
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			logger.Info("sampler: concurrent test", "index", index)
		}(i)
	}
	wg.Wait()
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	body := readLog(t, dir, chainPollFile)
	if got := strings.Count(body, "sampler: concurrent test"); got != count {
		t.Fatalf("written records=%d, want %d", got, count)
	}
}

func TestMarkdownRouterWithAttrsAndGroups(t *testing.T) {
	dir := t.TempDir()
	router, err := NewMarkdownRouter(slog.NewTextHandler(&bytes.Buffer{}, nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(router).With("service", "backend").WithGroup("market")
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "scan complete", 0)
	record.Add("game_id", 7)
	if err := logger.Handler().Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	body := readLog(t, dir, chainPoolFile)
	if !strings.Contains(body, "**market.service**：backend") || !strings.Contains(body, "**market.game_id**：7") {
		t.Fatalf("handler attributes missing:\n%s", body)
	}
}

func TestMarkdownRouterArchivesLegacyTableFormat(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, chainPoolFile)
	legacy := "# 旧日志\n\n| 时间 | 级别 | 消息 | 详情 |\n|---|---|---|---|\n| old | INFO | crowded | data |\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o640); err != nil {
		t.Fatal(err)
	}

	router, err := NewMarkdownRouter(slog.NewTextHandler(&bytes.Buffer{}, nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	slog.New(router).Info("sentinel round started", "round", 1)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	current := readLog(t, dir, chainPoolFile)
	if !strings.Contains(current, formatMarker) ||
		!strings.Contains(current, "## 🚀 后端会话") ||
		!strings.Contains(current, "### 🔄 第 1 轮") ||
		strings.Contains(current, "crowded") {
		t.Fatalf("new log was not cleanly migrated:\n%s", current)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "监听链上博弈池状态.旧格式-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("legacy archive count=%d, want 1", len(archives))
	}
	archived, err := os.ReadFile(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(archived), "crowded") {
		t.Fatalf("legacy content was not preserved:\n%s", archived)
	}
}

func readLog(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
