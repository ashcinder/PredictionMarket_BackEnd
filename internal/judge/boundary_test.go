package judge

import (
	"strings"
	"testing"
	"time"
)

func TestValidateVersion2RuleAcceptsBeijingDayBoundaries(t *testing.T) {
	start := beijingBoundary(2026, time.July, 14) // Tuesday
	rule := version2Rule(TypePrice, start, start.Add(24*time.Hour))
	rule.Direction = "UP"
	rule.FlatTolerance = 0.05
	if err := ValidateVersion2Rule(rule); err != nil {
		t.Fatal(err)
	}
}

func TestValidateVersion2RuleRejectsUnsafeBoundariesAndMetadata(t *testing.T) {
	start := beijingBoundary(2026, time.July, 14)
	valid := version2Rule(TypePrice, start, start.Add(24*time.Hour))
	valid.Direction, valid.FlatTolerance = "UP", 0.05
	tests := []struct {
		name string
		edit func(*Rule)
		want string
	}{
		{"sub-day", func(r *Rule) { r.EndTimeSec = r.StartTimeSec }, "at least one day"},
		{"not midnight", func(r *Rule) { r.StartTimeSec += 60 }, "midnight"},
		{"over four days", func(r *Rule) {
			r.StartTimeSec = beijingBoundary(2026, time.July, 16).Unix()
			r.EndTimeSec = beijingBoundary(2026, time.July, 21).Unix()
		}, "no more than four days"},
		{"Sunday", func(r *Rule) {
			r.StartTimeSec = beijingBoundary(2026, time.July, 19).Unix()
			r.EndTimeSec = beijingBoundary(2026, time.July, 21).Unix()
		}, "Tuesday through Saturday"},
		{"source", func(r *Rule) { r.Source = "GOLD_API" }, "source"},
		{"feed", func(r *Rule) { r.SourceContract = ChainlinkBTCUSDFeed }, "source_contract"},
		{"policy", func(r *Rule) { r.BoundaryPolicy = "NEAREST" }, "boundary_policy"},
		{"unsupported type", func(r *Rule) { r.Type = TypeEvent }, "unsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := valid
			tt.edit(&rule)
			err := ValidateVersion2Rule(rule)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateVersion2RuleAcceptsEverySupportedRelativeBenchmark(t *testing.T) {
	start := beijingBoundary(2026, time.July, 14)
	for _, symbol := range []string{"BTC", "ETH", "SOL", "BNB"} {
		t.Run(symbol, func(t *testing.T) {
			feed, ok := RelativeBenchmarkFeed(symbol)
			if !ok {
				t.Fatalf("missing feed for %s", symbol)
			}
			rule := version2Rule(TypeRelative, start, start.Add(24*time.Hour))
			rule.Benchmark, rule.BenchmarkSourceContract = symbol, feed
			if err := ValidateVersion2Rule(rule); err != nil {
				t.Fatal(err)
			}
		})
	}
}
