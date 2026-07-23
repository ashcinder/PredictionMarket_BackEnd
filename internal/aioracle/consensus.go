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
	if strings.TrimSpace(e.cfg.FinalArbiter) != "" {
		return e.judgeWithFinalArbiter(ctx, event, articles)
	}
	opinions := QueryAllModels(ctx, e.providers, event, articles)
	return e.aggregate(event, opinions)
}

func (e *ConsensusEngine) judgeWithFinalArbiter(ctx context.Context, event Event, articles []NewsArticle) *Verdict {
	arbiterName := strings.TrimSpace(e.cfg.FinalArbiter)
	peers := make([]ModelProvider, 0, len(e.providers)-1)
	var arbiter FinalModelProvider
	var configuredArbiter ModelProvider
	for _, provider := range e.providers {
		if strings.EqualFold(provider.Name(), arbiterName) {
			configuredArbiter = provider
			arbiter, _ = provider.(FinalModelProvider)
			continue
		}
		peers = append(peers, provider)
	}
	base := &Verdict{
		EventID: event.ID, Decision: DecisionIndeterminate,
		TotalModels: len(e.providers), ResolvedAt: time.Now(),
	}
	if configuredArbiter == nil {
		base.Summary = fmt.Sprintf("final arbiter %q is not configured", arbiterName)
		slog.Warn("aioracle: final adjudication unavailable",
			"stage", "final_adjudication",
			"event_id", event.ID,
			"final_arbiter", arbiterName,
			"logic_summary", "The configured final arbiter does not exist; remain INDETERMINATE and do not settle on-chain",
		)
		return base
	}
	if arbiter == nil {
		base.Summary = fmt.Sprintf("final arbiter %q does not support final adjudication", arbiterName)
		slog.Warn("aioracle: final adjudication unavailable",
			"stage", "final_adjudication",
			"event_id", event.ID,
			"final_arbiter", arbiterName,
			"logic_summary", "The configured model does not support final arbitration; remain INDETERMINATE and do not settle on-chain",
		)
		return base
	}

	slog.Info("aioracle: peer deliberation started",
		"stage", "independent_review",
		"event_id", event.ID,
		"peer_models", len(peers),
		"final_arbiter", arbiter.Name(),
		"evidence_items", len(articles),
		"logic_summary", "N-1 models first judge the same event definition and evidence independently without sharing answers",
	)
	opinions := QueryAllModels(ctx, peers, event, articles)
	for _, opinion := range opinions {
		if opinion.Error != "" {
			slog.Warn("aioracle: peer opinion failed",
				"stage", "independent_review",
				"event_id", event.ID,
				"model", opinion.ModelName,
				"model_id", opinion.ModelID,
				"error", opinion.Error,
				"logic_summary", "This model call or parse failed; record an abstention and pass the failure unchanged to the final arbiter",
			)
			continue
		}
		slog.Info("aioracle: peer opinion completed",
			"stage", "independent_review",
			"event_id", event.ID,
			"model", opinion.ModelName,
			"model_id", opinion.ModelID,
			"decision", opinion.Decision,
			"confidence", opinion.Confidence,
			"reasoning", opinion.Reasoning,
			"sources", strings.Join(opinion.Sources, ", "),
			"logic_summary", opinion.Reasoning,
		)
	}
	peerSummary := summarizeOpinions(opinions)

	slog.Info("aioracle: final adjudication requested",
		"stage", "final_handoff",
		"event_id", event.ID,
		"final_arbiter", arbiter.Name(),
		"opinions_received", len(opinions),
		"peer_opinions_summary", peerSummary,
		"logic_summary", "Send the event, all evidence, N-1 conclusions, reasoning and failures to the Nth model for independent final review; no backend confidence threshold substitutes for arbitration",
	)
	judgment, err := arbiter.QueryFinal(ctx, event, articles, opinions)
	if err != nil || judgment == nil {
		if err == nil {
			err = fmt.Errorf("empty final judgment")
		}
		opinions = append(opinions, ModelOpinion{
			ModelName: arbiter.Name(), ModelID: arbiter.ModelID(),
			Decision: DecisionIndeterminate, IsFinal: true, Error: err.Error(),
		})
		base.Opinions = opinions
		base.Summary = fmt.Sprintf("final arbiter %s failed: %v", arbiter.Name(), err)
		slog.Warn("aioracle: final adjudication failed",
			"stage", "final_adjudication",
			"event_id", event.ID,
			"final_arbiter", arbiter.Name(),
			"peer_opinions_summary", peerSummary,
			"error", err,
			"logic_summary", "Final-arbiter call or parsing failed; remain INDETERMINATE and do not settle on-chain",
		)
		return base
	}
	if judgment.Decision != DecisionYes && judgment.Decision != DecisionNo && judgment.Decision != DecisionIndeterminate {
		err = fmt.Errorf("invalid decision %q", judgment.Decision)
		opinions = append(opinions, ModelOpinion{
			ModelName: arbiter.Name(), ModelID: arbiter.ModelID(),
			Decision: DecisionIndeterminate, IsFinal: true, Error: err.Error(),
		})
		base.Opinions = opinions
		base.Summary = fmt.Sprintf("final arbiter %s failed: %v", arbiter.Name(), err)
		slog.Warn("aioracle: final adjudication failed",
			"stage", "final_adjudication",
			"event_id", event.ID,
			"final_arbiter", arbiter.Name(),
			"peer_opinions_summary", peerSummary,
			"error", err,
			"logic_summary", "Final arbiter returned an invalid decision; remain INDETERMINATE and do not settle on-chain",
		)
		return base
	}
	judgment.Confidence = math.Max(0, math.Min(1, judgment.Confidence))
	finalOpinion := ModelOpinion{
		ModelName: arbiter.Name(), ModelID: arbiter.ModelID(),
		Occurred: judgment.Decision == DecisionYes,
		Decision: judgment.Decision, Confidence: judgment.Confidence,
		Reasoning: judgment.Reasoning, Sources: judgment.Sources, IsFinal: true,
	}
	opinions = append(opinions, finalOpinion)
	base.Opinions = opinions
	base.Decision = judgment.Decision
	base.Occurred = judgment.Decision == DecisionYes
	base.Resolved = judgment.Decision == DecisionYes || judgment.Decision == DecisionNo
	base.Confidence = judgment.Confidence
	if judgment.Decision == DecisionIndeterminate {
		base.AgreeingModels = 0
		base.ConsensusRatio = 0
		base.Summary = fmt.Sprintf(
			"final arbiter %s returned INDETERMINATE after reviewing %d peer opinions; confidence %.2f; reason: %s",
			arbiter.Name(), len(opinions)-1, judgment.Confidence, judgment.Reasoning,
		)
		slog.Info("aioracle: final adjudication completed",
			"stage", "final_adjudication",
			"event_id", event.ID,
			"final_arbiter", arbiter.Name(),
			"decision", judgment.Decision,
			"resolved", false,
			"confidence", judgment.Confidence,
			"peer_agreement", 0,
			"reasoning", judgment.Reasoning,
			"sources", strings.Join(judgment.Sources, ", "),
			"peer_opinions_summary", peerSummary,
			"logic_summary", judgment.Reasoning,
		)
		return base
	}

	validModels := 1
	agreeingModels := 1
	for _, opinion := range opinions[:len(opinions)-1] {
		if opinion.Error != "" || opinion.Decision == DecisionIndeterminate {
			continue
		}
		validModels++
		if opinion.Decision == judgment.Decision {
			agreeingModels++
		}
	}
	base.AgreeingModels = agreeingModels
	base.ConsensusRatio = float64(agreeingModels) / float64(validModels)
	base.Summary = fmt.Sprintf(
		"final arbiter %s decided %s after reviewing %d peer opinions; confidence %.2f; peer agreement %.0f%%; reason: %s",
		arbiter.Name(), judgment.Decision, len(opinions)-1, judgment.Confidence,
		base.ConsensusRatio*100, judgment.Reasoning,
	)
	slog.Info("aioracle: final adjudication completed",
		"stage", "final_adjudication",
		"event_id", event.ID,
		"final_arbiter", arbiter.Name(),
		"decision", judgment.Decision,
		"resolved", base.Resolved,
		"confidence", judgment.Confidence,
		"peer_agreement", base.ConsensusRatio,
		"reasoning", judgment.Reasoning,
		"sources", strings.Join(judgment.Sources, ", "),
		"peer_opinions_summary", peerSummary,
		"logic_summary", judgment.Reasoning,
	)
	return base
}

func summarizeOpinions(opinions []ModelOpinion) string {
	if len(opinions) == 0 {
		return "No peer-model opinions"
	}
	parts := make([]string, 0, len(opinions))
	for _, opinion := range opinions {
		identity := strings.TrimSpace(opinion.ModelName)
		if modelID := strings.TrimSpace(opinion.ModelID); modelID != "" {
			identity += "/" + modelID
		}
		if opinion.Error != "" {
			parts = append(parts, fmt.Sprintf("%s=ERROR(%s)", identity, truncateContent(opinion.Error, 300)))
			continue
		}
		parts = append(parts, fmt.Sprintf(
			"%s=%s (confidence %.2f): %s",
			identity, opinion.Decision, opinion.Confidence,
			truncateContent(opinion.Reasoning, 600),
		))
	}
	return strings.Join(parts, "；")
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
		EventID:     event.ID,
		Occurred:    false,
		Decision:    DecisionIndeterminate,
		Resolved:    false,
		TotalModels: totalModels,
		Opinions:    append(valid, failed...),
		ResolvedAt:  now,
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
		weightOccurred        float64
		weightNotOccurred     float64
		weightTotal           float64
		confidenceOccurred    float64
		confidenceNotOccurred float64
		countOccurred         int
		countNotOccurred      int
	)

	for _, op := range valid {
		w := weights[op.ModelName]
		if w <= 0 {
			w = 1.0
		}
		weightTotal += w

		if op.Occurred {
			countOccurred++
			weightOccurred += w
			confidenceOccurred += w * op.Confidence
		} else {
			countNotOccurred++
			weightNotOccurred += w
			confidenceNotOccurred += w * op.Confidence
		}
	}

	if weightTotal == 0 {
		verdict.Summary = "all model weights are zero"
		return verdict
	}

	// Determine which side won.
	var (
		winnerOccurred bool
		winnerWeight   float64
		winnerCount    int
		winnerConfSum  float64
	)

	isTie := math.Abs(weightOccurred-weightNotOccurred) < 1e-9
	if weightOccurred > weightNotOccurred {
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

	verdict.AgreeingModels = winnerCount

	// Consensus ratio is based on configured provider weights. Confidence is a
	// separate threshold; mixing it into the vote made low-confidence dissent
	// artificially disappear from the denominator.
	if weightTotal > 0 {
		verdict.ConsensusRatio = winnerWeight / weightTotal
	}
	if verdict.ConsensusRatio > 1.0 {
		verdict.ConsensusRatio = 1.0
	}

	// Aggregated confidence is the provider-weighted average on the winning side.
	if winnerWeight > 0 {
		verdict.Confidence = winnerConfSum / winnerWeight
	}
	if verdict.Confidence > 1.0 {
		verdict.Confidence = 1.0
	}
	if math.IsNaN(verdict.Confidence) {
		verdict.Confidence = 0
	}

	// Handle ties BEFORE threshold checks: if weights are effectively equal,
	// delegate to the designated tiebreak model.
	if isTie && e.cfg.TiebreakModel != "" {
		for _, op := range valid {
			if strings.EqualFold(op.ModelName, e.cfg.TiebreakModel) && op.Error == "" {
				verdict.Confidence = op.Confidence
				verdict.ConsensusRatio = 1.0 // tiebreak model's verdict is treated as authoritative
				verdict.AgreeingModels = 1
				if op.Confidence < e.cfg.MinConfidence {
					verdict.Summary = fmt.Sprintf(
						"tiebreak model %s confidence %.2f below threshold %.2f",
						e.cfg.TiebreakModel, op.Confidence, e.cfg.MinConfidence,
					)
					return verdict
				}
				setResolvedDecision(verdict, op.Occurred)
				verdict.Summary = fmt.Sprintf(
					"tie broken by %s: %s (confidence %.2f)",
					e.cfg.TiebreakModel, boolLabel(op.Occurred), op.Confidence,
				)
				return verdict
			}
		}
	}

	if isTie {
		verdict.Summary = "indeterminate: weighted vote is tied and no eligible tiebreak model resolved it"
		return verdict
	}

	// Check thresholds.
	if verdict.ConsensusRatio < e.cfg.MinConsensusRatio {
		verdict.Summary = fmt.Sprintf(
			"consensus ratio %.2f below threshold %.2f (%d/%d valid models agreed with %s)",
			verdict.ConsensusRatio, e.cfg.MinConsensusRatio,
			winnerCount, len(valid), boolLabel(winnerOccurred),
		)
		return verdict
	}

	if verdict.Confidence < e.cfg.MinConfidence {
		verdict.Summary = fmt.Sprintf(
			"aggregated confidence %.2f below threshold %.2f",
			verdict.Confidence, e.cfg.MinConfidence,
		)
		return verdict
	}

	setResolvedDecision(verdict, winnerOccurred)

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

func setResolvedDecision(verdict *Verdict, occurred bool) {
	verdict.Resolved = true
	verdict.Occurred = occurred
	if occurred {
		verdict.Decision = DecisionYes
	} else {
		verdict.Decision = DecisionNo
	}
}
