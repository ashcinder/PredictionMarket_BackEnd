// Package aioracle implements a multi-model AI consensus oracle for verifying
// real-world events and triggering smart contract settlement.
//
// It queries multiple independent LLM providers (DeepSeek, Claude, GPT) with
// the same event description + curated news articles, then aggregates their
// opinions through a weighted consensus mechanism. Only when the consensus
// confidence exceeds a configurable threshold is an event considered "verified".
package aioracle

import (
	"time"
)

// Event describes a real-world occurrence that the oracle needs to verify.
// Events are defined off-chain (e.g., in config or IPFS metadata) and fed
// into the oracle for resolution.
type Event struct {
	// ID uniquely identifies this event (e.g., a game ID or custom slug).
	ID string `json:"id"`

	// Title is a short human-readable name, e.g. "BTC exceeds $150,000 in June 2026".
	Title string `json:"title"`

	// Description provides full context: what constitutes "occurred", what
	// sources are authoritative, any edge cases to consider.
	Description string `json:"description"`

	// Keywords help the news fetcher find relevant articles.
	Keywords []string `json:"keywords"`

	// Deadline is the cutoff time after which the oracle will issue a final
	// verdict regardless of confidence (timeout resolution).
	Deadline time.Time `json:"deadline"`
}

// NewsArticle represents a single news item retrieved from an external source.
type NewsArticle struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"published_at"`
	Content     string    `json:"content"`
}

// ModelOpinion is a single AI model's judgment about whether an event occurred.
type ModelOpinion struct {
	// ModelName identifies the configured provider, e.g. "deepseek", "claude".
	// It is deliberately different from ModelID so provider weights and
	// tiebreak configuration can be matched reliably.
	ModelName string `json:"model_name"`

	// ModelID is the provider-specific model identifier, e.g. "deepseek-chat".
	ModelID string `json:"model_id,omitempty"`

	// Occurred is the model's binary judgment.
	Occurred bool `json:"occurred"`

	// Confidence is the model's self-reported certainty (0.0 to 1.0).
	Confidence float64 `json:"confidence"`

	// Reasoning explains the model's conclusion with specific evidence.
	Reasoning string `json:"reasoning"`

	// Sources lists specific URLs or references the model relied on.
	Sources []string `json:"sources,omitempty"`

	// Error is non-empty if the model failed to respond (counts as abstain).
	Error string `json:"error,omitempty"`
}

// Decision is the tri-state settlement result. INDETERMINATE is intentionally
// distinct from NO: a lack of evidence must never settle a market as NO.
type Decision string

const (
	DecisionYes           Decision = "YES"
	DecisionNo            Decision = "NO"
	DecisionIndeterminate Decision = "INDETERMINATE"
)

// Verdict is the final consensus output produced by the oracle after
// aggregating all model opinions.
type Verdict struct {
	// EventID matches the input event.
	EventID string `json:"event_id"`

	// Occurred is the final consensus judgment.
	// Deprecated for settlement decisions: use Decision and Resolved so that
	// NO can be distinguished from insufficient evidence.
	Occurred bool `json:"occurred"`

	// Decision is YES, NO, or INDETERMINATE.
	Decision Decision `json:"decision"`

	// Resolved is true only when all configured consensus and confidence
	// thresholds were met. Only resolved verdicts are safe to settle.
	Resolved bool `json:"resolved"`

	// Confidence is the aggregated confidence (0.0 to 1.0).
	Confidence float64 `json:"confidence"`

	// ConsensusRatio is what fraction of responding models agreed with the
	// majority outcome (0.0 to 1.0). A value of 1.0 means unanimous.
	ConsensusRatio float64 `json:"consensus_ratio"`

	// TotalModels is the number of models queried.
	TotalModels int `json:"total_models"`

	// AgreeingModels is how many models voted for the winning outcome.
	AgreeingModels int `json:"agreeing_models"`

	// Opinions holds every individual model opinion for auditability.
	Opinions []ModelOpinion `json:"opinions"`

	// Summary is a human-readable explanation of the consensus process.
	Summary string `json:"summary"`

	// ResolvedAt is when the verdict was produced.
	ResolvedAt time.Time `json:"resolved_at"`
}

// ProviderConfig holds the configuration for a single AI model provider.
type ProviderConfig struct {
	// Name is a human-readable label, e.g. "deepseek", "claude", "openai".
	Name string `yaml:"name" json:"name"`

	// Model is the specific model ID, e.g. "deepseek-chat", "claude-opus-4-8".
	Model string `yaml:"model" json:"model"`

	// APIKey authenticates with the provider.
	APIKey string `yaml:"api_key" json:"api_key"`

	// BaseURL is the chat completions endpoint.
	// DeepSeek: https://api.deepseek.com/chat/completions
	// Claude:   https://api.anthropic.com/v1/messages
	// OpenAI:   https://api.openai.com/v1/chat/completions
	BaseURL string `yaml:"base_url" json:"base_url"`

	// Provider identifies the API type: "deepseek", "anthropic", "openai".
	// This controls header format and request/response parsing.
	Provider string `yaml:"provider" json:"provider"`

	// Weight controls how much this model's opinion counts in consensus
	// (default 1.0). Higher weight = more influence.
	Weight float64 `yaml:"weight" json:"weight"`

	// TimeoutSeconds is the per-request deadline.
	TimeoutSeconds int `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// ConsensusConfig tunes how individual opinions are aggregated into a verdict.
type ConsensusConfig struct {
	// MinConsensusRatio is the minimum fraction of models that must agree
	// before a verdict is considered "occurred" (0.0 to 1.0).
	// Example: 0.66 means at least 2/3 of models must concur.
	MinConsensusRatio float64 `yaml:"min_consensus_ratio"`

	// MinConfidence is the threshold for the aggregated confidence score.
	// The final verdict confidence must exceed this for Occurred=true.
	MinConfidence float64 `yaml:"min_confidence"`

	// MinModelsRequired is the minimum number of models that must respond
	// successfully before a verdict can be issued. Fewer = "insufficient data".
	MinModelsRequired int `yaml:"min_models_required"`

	// TiebreakModel is an optional model name to use as a tiebreaker when
	// the vote is evenly split. Must match a provider's Name field.
	TiebreakModel string `yaml:"tiebreak_model"`
}

// NewsConfig configures news article fetching.
type NewsConfig struct {
	// NewsAPIKey is the API key for newsapi.org (optional).
	NewsAPIKey string `yaml:"news_api_key"`

	// NewsAPIURL overrides the default newsapi.org endpoint.
	NewsAPIURL string `yaml:"news_api_url"`

	// RSSFeeds is a list of RSS feed URLs to pull from.
	RSSFeeds []string `yaml:"rss_feeds"`

	// MaxArticles limits how many articles are included in the prompt (default 10).
	MaxArticles int `yaml:"max_articles"`

	// LookbackHours is how far back to search for news (default 72).
	LookbackHours int `yaml:"lookback_hours"`

	// RequestTimeoutSeconds is the timeout per news fetch.
	RequestTimeoutSeconds int `yaml:"request_timeout_seconds"`
}
