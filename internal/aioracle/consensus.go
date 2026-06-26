package aioracle

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
)

// ConsensusEngine aggregates individual model opinions into a final verdict
// using weighted majority voting with confidence thresholds.
type ConsensusEngine struct {
	cfg       ConsensusConfig
	providers []ModelProvider
}

// NewConsensusEngine creates a consensus engine with the given config and
// the set of model providers whose opinions will be aggregated.
func NewConsensusEngine(cfg ConsensusConfig, providers []ModelProvider) *ConsensusEngine {
	if cfg.MinConsensusRatio <= 0 {
		cfg.MinConsensusRatio = 0.66 // 2/3 majority by default
	}
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 0.60
	}
	if cfg.MinModelsRequired <= 0 {
		cfg.MinModelsRequired = 1
	}
	if len(providers) == 0 {
		slog.Warn("aioracle: consensus engine initialized with zero providers — all verdicts will be 'insufficient data'")
	}
	return &ConsensusEngine{
		cfg:       cfg,
		providers: providers,
	}
}

// Judge queries all configured models in parallel, aggregates their opinions,
// and returns a final verdict. If ctx is cancelled mid-flight, partial results
// are used if enough models have responded.
func (e *ConsensusEngine) Judge(ctx context.Context, event Event, articles []NewsArticle) *Verdict {
	opinions := QueryAllModels(ctx, e.providers, event, articles)
	return e.aggregate(event, opinions)
}

// JudgeWithOpinions allows passing pre-obtained opinions (e.g., from a cache
// or a previous partial run) for aggregation without re-querying models.
func (e *ConsensusEngine) JudgeWithOpinions(event Event, opinions []ModelOpinion) *Verdict {
	return e.aggregate(event, opinions)
}

// aggregate implements the weighted consensus algorithm.
func (e *ConsensusEngine) aggregate(event Event, opinions []ModelOpinion) *Verdict {
	totalModels := len(e.providers)
	now := time.Now()

	// Separate successful opinions from failures.
	var valid []ModelOpinion
	var failed []ModelOpinion
	for _, op := range opinions {
		if op.Error != "" {
			failed = append(failed, op)
		} else {
			valid = append(valid, op)
		}
	}

	verdict := &Verdict{
		EventID:    event.ID,
		Occurred:   false,
		TotalModels: totalModels,
		Opinions:   append(valid, failed...),
		ResolvedAt: now,
	}

	if len(valid) < e.cfg.MinModelsRequired {
		verdict.Summary = fmt.Sprintf(
			"insufficient data: %d/%d models responded successfully (need %d)",
			len(valid), totalModels, e.cfg.MinModelsRequired,
		)
		return verdict
	}

	// Build a weighted vote map: model name -> weight.
	weights := make(map[string]float64, len(e.providers))
	for _, p := range e.providers {
		weights[p.Name()] = p.Weight()
	}

	// Tally weighted votes for "occurred" and "not occurred".
	var (
		weightOccurred     float64
		weightNotOccurred  float64
		weightTotal        float64
		confidenceOccurred float64
		confidenceNotOccurred float64
		countOccurred      int
		countNotOccurred   int
	)

	for _, op := range valid {
		w := weights[op.ModelName]
		if w <= 0 {
			w = 1.0
		}
		weightTotal += w

		// Confidence is folded into the vote weight so high-confidence
		// opinions carry more influence.
		weightedVote := w * op.Confidence

		if op.Occurred {
			countOccurred++
			weightOccurred += weightedVote
			confidenceOccurred += op.Confidence
		} else {
			countNotOccurred++
			weightNotOccurred += weightedVote
			confidenceNotOccurred += op.Confidence
		}
	}

	if weightTotal == 0 {
		verdict.Summary = "all model weights are zero"
		return verdict
	}

	// Determine which side won.
	var (
		winnerOccurred   bool
		winnerWeight     float64
		winnerCount      int
		winnerConfSum    float64
		totalWeighted    = weightOccurred + weightNotOccurred
	)

	if weightOccurred >= weightNotOccurred {
		winnerOccurred = true
		winnerWeight = weightOccurred
		winnerCount = countOccurred
		winnerConfSum = confidenceOccurred
	} else {
		winnerOccurred = false
		winnerWeight = weightNotOccurred
		winnerCount = countNotOccurred
		winnerConfSum = confidenceNotOccurred
	}

	verdict.Occurred = winnerOccurred
	verdict.AgreeingModels = winnerCount

	// Consensus ratio: what fraction of total weighted vote went to the winner?
	if totalWeighted > 0 {
		verdict.ConsensusRatio = winnerWeight / totalWeighted
	}
	if verdict.ConsensusRatio > 1.0 {
		verdict.ConsensusRatio = 1.0
	}

	// Aggregated confidence: weighted average of winner-side confidence,
	// scaled by the consensus ratio (stronger consensus = higher final confidence).
	if winnerCount > 0 {
		verdict.Confidence = (winnerConfSum / float64(winnerCount)) * verdict.ConsensusRatio
	}
	if verdict.Confidence > 1.0 {
		verdict.Confidence = 1.0
	}
	if math.IsNaN(verdict.Confidence) {
		verdict.Confidence = 0
	}

	// Handle ties BEFORE threshold checks: if weights are effectively equal,
	// delegate to the designated tiebreak model.
	if math.Abs(weightOccurred-weightNotOccurred) < 1e-9 && e.cfg.TiebreakModel != "" {
		for _, op := range valid {
			if strings.EqualFold(op.ModelName, e.cfg.TiebreakModel) && op.Error == "" {
				verdict.Occurred = op.Occurred
				verdict.Confidence = op.Confidence
				verdict.ConsensusRatio = 1.0 // tiebreak model's verdict is treated as authoritative
				verdict.AgreeingModels = 1
				verdict.Summary = fmt.Sprintf(
					"tie broken by %s: %s (confidence %.2f)",
					e.cfg.TiebreakModel, boolLabel(op.Occurred), op.Confidence,
				)
				return verdict
			}
		}
	}

	// Check thresholds.
	if verdict.ConsensusRatio < e.cfg.MinConsensusRatio {
		verdict.Occurred = false
		verdict.Summary = fmt.Sprintf(
			"consensus ratio %.2f below threshold %.2f (%d/%d valid models agreed with %s)",
			verdict.ConsensusRatio, e.cfg.MinConsensusRatio,
			winnerCount, len(valid), boolLabel(winnerOccurred),
		)
		return verdict
	}

	if verdict.Confidence < e.cfg.MinConfidence {
		verdict.Occurred = false
		verdict.Summary = fmt.Sprintf(
			"aggregated confidence %.2f below threshold %.2f",
			verdict.Confidence, e.cfg.MinConfidence,
		)
		return verdict
	}

	// Build summary.
	var parts []string
	for _, op := range valid {
		parts = append(parts, fmt.Sprintf("%s=%s(%.2f)", op.ModelName, boolLabel(op.Occurred), op.Confidence))
	}
	for _, op := range failed {
		parts = append(parts, fmt.Sprintf("%s=ERROR(%s)", op.ModelName, op.Error))
	}

	verdict.Summary = fmt.Sprintf(
		"verdict: %s | confidence: %.2f | consensus: %.0f%% (%d/%d models) | votes: [%s]",
		boolLabel(verdict.Occurred),
		verdict.Confidence,
		verdict.ConsensusRatio*100,
		winnerCount,
		len(valid),
		strings.Join(parts, ", "),
	)

	return verdict
}

// ConsensusConfig returns a copy of the engine's config.
func (e *ConsensusEngine) ConsensusConfig() ConsensusConfig { return e.cfg }

// ProviderCount returns how many models are registered.
func (e *ConsensusEngine) ProviderCount() int { return len(e.providers) }

func boolLabel(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}
