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
	logger.Info("ai-managed decision reasoning trace",
		"stage", "risk_evaluation",
		"logic_summary", "模型概率高于市场概率，继续检查置信度与冷却期",
		"decision", "buy_yes",
	)
	logger.Info("aioracle: peer opinion completed", "model", "glm", "decision", "YES", "reasoning", "权威信源确认")
	logger.Info("aioracle: final reasoning trace",
		"model", "minimax", "decision", "YES",
		"logic_summary", "核对两份前序意见和权威证据后终审",
	)
	logger.Info("sampler: cycle complete", "games", 3, "api_key", "sk-abcdefghijklmnop")
	logger.Info("unclassified general log", "value", 1)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}

	chain := readLog(t, dir, chainPoolFile)
	managed := readLog(t, dir, aiManagedFile)
	oracle := readLog(t, dir, aiOracleFile)
	poll := readLog(t, dir, chainPollFile)
	if !strings.Contains(chain, "prediction market sentinel started") {
		t.Fatalf("chain log missing routed message:\n%s", chain)
	}
	if !strings.Contains(chain, "### 轮次 1") ||
		!strings.Contains(chain, "### 轮次 2") ||
		!strings.Contains(chain, "## 后端会话") ||
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
	if !strings.Contains(managed, "ai-managed decision reasoning trace") ||
		!strings.Contains(managed, "模型概率高于市场概率") {
		t.Fatalf("managed reasoning trace is incomplete:\n%s", managed)
	}
	if !strings.Contains(oracle, "aioracle: peer opinion completed") ||
		!strings.Contains(oracle, "权威信源确认") ||
		!strings.Contains(oracle, "核对两份前序意见和权威证据后终审") {
		t.Fatalf("AI oracle audit log is incomplete:\n%s", oracle)
	}
	if strings.Contains(chain, "aioracle: peer opinion completed") {
		t.Fatalf("AI oracle details should use their own log file:\n%s", chain)
	}
	if !strings.Contains(poll, "sampler: cycle complete") ||
		strings.Contains(poll, "sk-abcdefghijklmnop") {
		t.Fatalf("poll log routing/redaction failed:\n%s", poll)
	}
	for _, body := range []string{chain, managed, oracle, poll} {
		for _, icon := range []string{"🚀", "🔄", "🔴", "🟠", "🔵", "⚪"} {
			if strings.Contains(body, icon) {
				t.Fatalf("log should use plain text instead of icon %q:\n%s", icon, body)
			}
		}
		if strings.Contains(body, "unclassified general log") {
			t.Fatalf("unclassified message should not be written to category logs:\n%s", body)
		}
	}
	if !strings.Contains(console.String(), "unclassified general log") {
		t.Fatal("console handler did not receive all logs")
	}
}

func TestMarkdownRouterRebuildsLogsForEveryBackendRun(t *testing.T) {
	dir := t.TempDir()
	first, err := NewMarkdownRouter(slog.NewTextHandler(&bytes.Buffer{}, nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	slog.New(first).Info("ai-managed first-run marker")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := NewMarkdownRouter(slog.NewTextHandler(&bytes.Buffer{}, nil), dir)
	if err != nil {
		t.Fatal(err)
	}
	slog.New(second).Info("ai-managed second-run marker")
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	body := readLog(t, dir, aiManagedFile)
	if strings.Contains(body, "first-run marker") {
		t.Fatalf("previous backend run was not cleared:\n%s", body)
	}
	if !strings.Contains(body, "second-run marker") {
		t.Fatalf("current backend run is missing:\n%s", body)
	}
	if strings.Count(body, formatMarker) != 1 || strings.Count(body, "## 后端会话") != 1 {
		t.Fatalf("log file was appended instead of rebuilt:\n%s", body)
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

func TestMarkdownRouterReplacesLegacyTableFormat(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, chainPoolFile)
	legacy := "# Legacy log\n\n| Time | Level | Message | Details |\n|---|---|---|---|\n| old | INFO | crowded | data |\n"
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
		!strings.Contains(current, "## 后端会话") ||
		!strings.Contains(current, "### 轮次 1") ||
		strings.Contains(current, "crowded") {
		t.Fatalf("new log was not cleanly migrated:\n%s", current)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "chain-market-monitor.legacy-format-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("legacy log should be cleared instead of archived: %v", archives)
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
