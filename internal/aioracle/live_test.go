package aioracle

import (
	"context"
	"os"
	"testing"
	"time"

	"PredictionMarket/internal/config"
)

type liveFixtureNewsFetcher struct{}

func (liveFixtureNewsFetcher) Fetch(_ context.Context, _ []string, _ time.Time, _ int) ([]NewsArticle, error) {
	return []NewsArticle{{
		Title:       "AI Oracle smoke-test marker observed",
		URL:         "https://example.invalid/aioracle-smoke-test",
		Source:      "Local AI Oracle Test Fixture",
		PublishedAt: time.Now(),
		Content:     "The authoritative synthetic marker AI_ORACLE_SMOKE_OK was observed.",
	}}, nil
}

// Run with AIORACLE_LIVE=1 from the repository root. This intentionally makes
// paid external API requests and is therefore skipped during normal test runs.
func TestLiveConfiguredOracle(t *testing.T) {
	if os.Getenv("AIORACLE_LIVE") != "1" {
		t.Skip("set AIORACLE_LIVE=1 to call configured AI providers")
	}

	cfg, err := config.LoadFile("../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}

	providerConfigs := make([]ProviderConfig, 0, len(cfg.AIOracleProviders))
	for _, p := range cfg.AIOracleProviders {
		providerConfigs = append(providerConfigs, ProviderConfig{
			Name:           p.Name,
			Model:          p.Model,
			APIKey:         p.APIKey,
			BaseURL:        p.BaseURL,
			Provider:       p.Provider,
			Weight:         p.Weight,
			TimeoutSeconds: p.TimeoutSeconds,
		})
	}
	providers := NewProviders(providerConfigs)
	if len(providers) != len(providerConfigs) {
		t.Fatalf("initialized %d/%d configured providers", len(providers), len(providerConfigs))
	}

	engine := NewConsensusEngine(ConsensusConfig{
		MinConsensusRatio: cfg.AIOracleConsensus.MinConsensusRatio,
		MinConfidence:     cfg.AIOracleConsensus.MinConfidence,
		MinModelsRequired: cfg.AIOracleConsensus.MinModelsRequired,
		TiebreakModel:     cfg.AIOracleConsensus.TiebreakModel,
	}, providers)
	oracle := NewOracle(liveFixtureNewsFetcher{}, engine, time.Duration(cfg.AIOraclePollIntervalSeconds)*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	verdict := oracle.Resolve(ctx, Event{
		ID:    "live-connectivity-smoke-test",
		Title: "AI Oracle API connectivity smoke test",
		Description: "这是一个合成连通性测试，不是现实世界预测。" +
			"仅在所提供的 Local AI Oracle Test Fixture 中出现标记 AI_ORACLE_SMOKE_OK 时视为发生；" +
			"该测试 fixture 是此合成事件唯一指定的权威来源。",
		Keywords: []string{"synthetic connectivity test"},
		Deadline: time.Now().Add(time.Hour),
	})

	successful := 0
	for _, opinion := range verdict.Opinions {
		if opinion.Error != "" {
			t.Errorf("%s (%s) failed: %s", opinion.ModelName, opinion.ModelID, opinion.Error)
			continue
		}
		successful++
		t.Logf("%s (%s): occurred=%v confidence=%.2f",
			opinion.ModelName, opinion.ModelID, opinion.Occurred, opinion.Confidence)
	}
	if successful != len(providers) {
		t.Fatalf("only %d/%d providers completed successfully", successful, len(providers))
	}
	if !verdict.Resolved || verdict.Decision != DecisionYes {
		t.Fatalf("full consensus path did not resolve YES: decision=%s confidence=%.2f ratio=%.2f summary=%s",
			verdict.Decision, verdict.Confidence, verdict.ConsensusRatio, verdict.Summary)
	}
	t.Logf("consensus: decision=%s confidence=%.2f ratio=%.2f",
		verdict.Decision, verdict.Confidence, verdict.ConsensusRatio)
}
