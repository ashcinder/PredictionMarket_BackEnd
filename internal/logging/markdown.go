package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	chainPoolFile = "chain-market-monitor.md"
	aiManagedFile = "ai-management.md"
	aiOracleFile  = "ai-resolution.md"
	chainPollFile = "chain-data-polling.md"
	formatMarker  = "<!-- prediction-market-log-format: 2 -->"
)

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type category string

const (
	categoryChainPool category = "chain_pool"
	categoryAIManaged category = "ai_managed"
	categoryAIOracle  category = "ai_oracle"
	categoryChainPoll category = "chain_poll"
)

type markdownSink struct {
	mu   sync.Mutex
	file *os.File
}

// MarkdownRouter preserves normal console logging and additionally routes
// selected runtime records into Markdown files rebuilt for each backend run.
type MarkdownRouter struct {
	console slog.Handler
	sinks   map[category]*markdownSink
	attrs   []slog.Attr
	groups  []string
}

func NewMarkdownRouter(console slog.Handler, dir string) (*MarkdownRouter, error) {
	if console == nil {
		console = slog.NewTextHandler(io.Discard, nil)
	}
	if strings.TrimSpace(dir) == "" {
		dir = "logs"
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create markdown log directory: %w", err)
	}

	specs := []struct {
		category category
		filename string
		title    string
	}{
		{categoryChainPool, chainPoolFile, "On-chain Market Monitoring Log"},
		{categoryAIManaged, aiManagedFile, "AI Management Log"},
		{categoryAIOracle, aiOracleFile, "Multi-AI Resolution Log"},
		{categoryChainPoll, chainPollFile, "On-chain Data Polling Log"},
	}
	router := &MarkdownRouter{
		console: console,
		sinks:   make(map[category]*markdownSink, len(specs)),
	}
	for _, spec := range specs {
		path := filepath.Join(dir, spec.filename)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			router.Close()
			return nil, fmt.Errorf("open markdown log %s: %w", path, err)
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			router.Close()
			return nil, fmt.Errorf("stat markdown log %s: %w", path, err)
		}
		if info.Size() == 0 {
			header := fmt.Sprintf("%s\n\n# %s\n\n_Current backend run · Asia/Shanghai · Each event is shown separately_\n", formatMarker, spec.title)
			if _, err := file.WriteString(header); err != nil {
				file.Close()
				router.Close()
				return nil, fmt.Errorf("initialize markdown log %s: %w", path, err)
			}
		}
		session := fmt.Sprintf("\n---\n\n## Backend Session · %s\n\n### Startup\n\n",
			time.Now().In(shanghaiLocation).Format("2006-01-02 15:04:05"),
		)
		if _, err := file.WriteString(session); err != nil {
			file.Close()
			router.Close()
			return nil, fmt.Errorf("start markdown log session %s: %w", path, err)
		}
		router.sinks[spec.category] = &markdownSink{file: file}
	}
	return router, nil
}

func (h *MarkdownRouter) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level)
}

func (h *MarkdownRouter) Handle(ctx context.Context, record slog.Record) error {
	consoleErr := h.console.Handle(ctx, record)
	route := classify(record, h.attrs)
	if route == "" {
		return consoleErr
	}
	sink := h.sinks[route]
	if sink == nil {
		return consoleErr
	}
	if err := sink.write(record, h.attrs, h.groups); err != nil {
		return err
	}
	return consoleErr
}

func (h *MarkdownRouter) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := h.clone()
	clone.console = h.console.WithAttrs(attrs)
	clone.attrs = append(clone.attrs, attrs...)
	return clone
}

func (h *MarkdownRouter) WithGroup(name string) slog.Handler {
	clone := h.clone()
	clone.console = h.console.WithGroup(name)
	if name != "" {
		clone.groups = append(clone.groups, name)
	}
	return clone
}

func (h *MarkdownRouter) Close() error {
	var first error
	closed := make(map[*markdownSink]bool)
	for _, sink := range h.sinks {
		if sink == nil || closed[sink] {
			continue
		}
		closed[sink] = true
		sink.mu.Lock()
		err := sink.file.Close()
		sink.mu.Unlock()
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (h *MarkdownRouter) clone() *MarkdownRouter {
	return &MarkdownRouter{
		console: h.console,
		sinks:   h.sinks,
		attrs:   append([]slog.Attr(nil), h.attrs...),
		groups:  append([]string(nil), h.groups...),
	}
}

func (s *markdownSink) write(record slog.Record, baseAttrs []slog.Attr, groups []string) error {
	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	timestamp = timestamp.In(shanghaiLocation)
	attrs := append([]slog.Attr(nil), baseAttrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	if isRoundStart(record.Message) {
		round := attrString(attrs, "round")
		if round == "" {
			round = "?"
		}
		section := fmt.Sprintf("\n---\n\n### Round %s\n\n**Started at:** %s\n\n",
			markdownEscape(round),
			timestamp.Format("2006-01-02 15:04:05"),
		)
		contextDetails := formatAttrLines(attrsWithout(attrs, "round"), groups)
		if contextDetails != "" {
			section += "**Initial round state**\n\n" + contextDetails + "\n"
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		_, err := s.file.WriteString(section)
		return err
	}

	level := strings.ToUpper(record.Level.String())
	line := fmt.Sprintf("#### %s · %s\n\n**Event:** %s\n\n",
		markdownEscape(level),
		timestamp.Format("15:04:05.000"),
		markdownEscape(redactText(record.Message)),
	)
	if details := formatAttrLines(attrs, groups); details != "" {
		line += details
	} else {
		line += "_No additional data_\n"
	}
	line += "\n"
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.file.WriteString(line)
	return err
}

func attrsWithout(attrs []slog.Attr, excludedKey string) []slog.Attr {
	out := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if !strings.EqualFold(attr.Key, excludedKey) {
			out = append(out, attr)
		}
	}
	return out
}

func isRoundStart(message string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(message)), "round started")
}

func attrString(attrs []slog.Attr, key string) string {
	for _, attr := range attrs {
		if strings.EqualFold(attr.Key, key) {
			return fmt.Sprint(attr.Value.Resolve().Any())
		}
	}
	return ""
}

func classify(record slog.Record, baseAttrs []slog.Attr) category {
	message := strings.ToLower(record.Message)
	attrText := strings.ToLower(formatAttrs(baseAttrs, nil))
	record.Attrs(func(attr slog.Attr) bool {
		attrText += " " + strings.ToLower(attr.Key) + "=" + strings.ToLower(fmt.Sprint(attr.Value.Any()))
		return true
	})

	switch {
	case strings.Contains(message, "sampler") ||
		strings.Contains(message, "market history") ||
		strings.Contains(message, "history handler"):
		return categoryChainPoll
	case strings.Contains(message, "ai-managed") ||
		strings.Contains(message, "ai managed") ||
		strings.Contains(message, "mysql repository:") ||
		strings.Contains(attrText, "/ai-managed"):
		return categoryAIManaged
	case strings.Contains(message, "aioracle:") ||
		strings.Contains(message, "game evaluated by ai") ||
		strings.Contains(message, "ai oracle left game"):
		return categoryAIOracle
	case strings.Contains(message, "sentinel") ||
		strings.Contains(message, "scan complete") ||
		strings.Contains(message, "scan failed") ||
		strings.Contains(message, "resolve game") ||
		strings.Contains(message, "game evaluated") ||
		strings.Contains(message, "game resolved on chain"):
		return categoryChainPool
	default:
		return ""
	}
}

// NewConciseConsoleHandler keeps the terminal readable while MarkdownRouter
// still receives every enabled record. Warnings and errors are always shown.
func NewConciseConsoleHandler(writer io.Writer) slog.Handler {
	base := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo})
	return &conciseConsoleHandler{next: base}
}

type conciseConsoleHandler struct {
	next slog.Handler
}

func (h *conciseConsoleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *conciseConsoleHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= slog.LevelWarn || essentialTerminalMessage(record.Message) {
		return h.next.Handle(ctx, record)
	}
	return nil
}

func (h *conciseConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &conciseConsoleHandler{next: h.next.WithAttrs(attrs)}
}

func (h *conciseConsoleHandler) WithGroup(name string) slog.Handler {
	return &conciseConsoleHandler{next: h.next.WithGroup(name)}
}

func essentialTerminalMessage(message string) bool {
	message = strings.ToLower(message)
	for _, marker := range []string{
		"using brokerchain",
		"using local rpc",
		"http api server started",
		"entries restored",
		"engine started",
		"sentinel started",
		"sampler started",
		"sentinel round completed",
		"aioracle: verdict reached",
		"game evaluated by ai consensus",
		"game resolved on chain",
		"ai-managed trade executed",
		"service exited",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func formatAttrs(attrs []slog.Attr, groups []string) string {
	parts := make([]string, 0, len(attrs))
	prefix := strings.Join(groups, ".")
	for _, attr := range attrs {
		appendAttr(&parts, prefix, attr)
	}
	return strings.Join(parts, "; ")
}

func formatAttrLines(attrs []slog.Attr, groups []string) string {
	flat := formatAttrs(attrs, groups)
	if flat == "" {
		return ""
	}
	parts := strings.Split(flat, "; ")
	var output strings.Builder
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		fmt.Fprintf(&output, "- **%s**：%s\n",
			markdownEscape(key),
			markdownEscape(value),
		)
	}
	return output.String()
}

func appendAttr(parts *[]string, prefix string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	key := attr.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	if attr.Value.Kind() == slog.KindGroup {
		for _, child := range attr.Value.Group() {
			appendAttr(parts, key, child)
		}
		return
	}
	value := fmt.Sprint(attr.Value.Any())
	if sensitiveKey(key) {
		value = "[REDACTED]"
	} else {
		value = redactText(value)
	}
	*parts = append(*parts, key+"="+value)
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range []string{"private_key", "apikey", "api_key", "authorization", "token", "secret", "password", "dsn"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|private[_-]?key|authorization|token|secret|password)\s*[=:]\s*[^\s,;]+`),
}

func redactText(value string) string {
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}

func markdownEscape(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}
