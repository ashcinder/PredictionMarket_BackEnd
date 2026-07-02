package sentinel

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"PredictionMarket/internal/aioracle"
	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/config"
	"PredictionMarket/internal/ipfs"
)

type EventResolver interface {
	Resolve(ctx context.Context, event aioracle.Event) *aioracle.Verdict
}

type Watcher struct {
	cfg       *config.Config
	chain     *chain.Client
	ipfs      *ipfs.Client
	oracle    EventResolver
	resolving sync.Map
	round     atomic.Uint64
}

const aiResolutionTimeout = 90 * time.Second

func NewWatcher(cfg *config.Config, chainClient *chain.Client, ipfsClient *ipfs.Client, aiOracle EventResolver) *Watcher {
	return &Watcher{
		cfg:    cfg,
		chain:  chainClient,
		ipfs:   ipfsClient,
		oracle: aiOracle,
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	slog.Info("prediction market sentinel started",
		"contract", w.cfg.ContractAddress,
		"wallet", w.chain.WalletAddress(),
		"poll_interval", w.cfg.PollInterval.String(),
		"use_broker_chain", w.cfg.UseBrokerChain,
		"resolution_mode", "ai_consensus_only",
	)

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	if err := w.scanOnce(ctx); err != nil {
		slog.Warn("initial scan failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("sentinel stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := w.scanOnce(ctx); err != nil {
				slog.Warn("scan failed", "error", err)
			}
		}
	}
}

func (w *Watcher) scanOnce(ctx context.Context) error {
	round := w.round.Add(1)
	startedAt := time.Now()
	slog.Info("sentinel round started", "round", round)

	data := chain.EncodeGetAllGames()
	hexResult, err := w.chain.EthCall(ctx, data)
	if err != nil {
		return fmt.Errorf("eth_call getAllGames: %w", err)
	}
	games, err := chain.DecodeGetAllGames(hexResult)
	if err != nil {
		return fmt.Errorf("decode getAllGames: %w", err)
	}

	now := time.Now().UnixMilli()
	var pending, resolved, failed int
	for _, game := range games {
		if game.IsResolved || game.IsRefunded {
			continue
		}
		if !chain.IsDeadlinePassed(game.DeadlineRaw, now) {
			continue
		}
		pending++
		if err := w.resolveGame(ctx, game); err != nil {
			failed++
			slog.Error("resolve game failed", "game_id", game.ID, "error", err)
		} else {
			resolved++
		}
	}
	slog.Info("sentinel round completed",
		"round", round,
		"total_games", len(games),
		"pending_resolve", pending,
		"resolved", resolved,
		"failed", failed,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	return nil
}

func (w *Watcher) resolveGame(ctx context.Context, game chain.GameOnChain) error {
	key := fmt.Sprintf("%d", game.ID)
	if _, loaded := w.resolving.LoadOrStore(key, true); loaded {
		return nil
	}
	defer w.resolving.Delete(key)

	meta, err := w.ipfs.DownloadMetadata(game.IPFSCID)
	if err != nil {
		return fmt.Errorf("load ipfs metadata: %w", err)
	}
	condition := meta.Condition
	if condition == "" {
		return fmt.Errorf("game %d has empty condition in ipfs metadata", game.ID)
	}

	if w.oracle == nil {
		return fmt.Errorf("game %d: AI oracle is not configured", game.ID)
	}
	event := buildAIEvent(game, meta)
	resolveCtx, cancel := context.WithTimeout(ctx, aiResolutionTimeout)
	verdict := w.oracle.Resolve(resolveCtx, event)
	cancel()
	if verdict == nil {
		return fmt.Errorf("game %d: AI oracle returned no verdict", game.ID)
	}

	winner, err := winnerFromVerdict(verdict)
	if err != nil {
		slog.Warn("AI oracle left game unresolved",
			"game_id", game.ID,
			"decision", verdict.Decision,
			"confidence", verdict.Confidence,
			"consensus_ratio", verdict.ConsensusRatio,
			"summary", verdict.Summary,
		)
		return fmt.Errorf("game %d: %w", game.ID, err)
	}
	winnerName := optionName(meta, winner)

	slog.Info("game evaluated by AI consensus",
		"game_id", game.ID,
		"condition", condition,
		"winner_index", winner,
		"winner", winnerName,
		"confidence", verdict.Confidence,
		"consensus_ratio", verdict.ConsensusRatio,
		"agreeing_models", verdict.AgreeingModels,
		"total_models", verdict.TotalModels,
		"summary", verdict.Summary,
	)

	if w.cfg.ResolveDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.cfg.ResolveDelay):
		}
	}

	txData := chain.EncodeResolveGame(game.ID, winner)

	txResult, err := w.chain.SendTransaction(ctx, txData, nil)
	if err != nil {
		return fmt.Errorf("send resolveGame tx: %w", err)
	}

	slog.Info("game resolved on chain",
		"game_id", game.ID,
		"winner", winnerName,
		"tx", txResult,
	)
	return nil
}

func buildAIEvent(game chain.GameOnChain, meta *ipfs.Metadata) aioracle.Event {
	title := strings.TrimSpace(meta.Desc)
	if title == "" {
		title = strings.TrimSpace(meta.Condition)
	}
	yesOption := strings.TrimSpace(meta.OptionYES)
	if yesOption == "" {
		yesOption = "YES"
	}
	noOption := strings.TrimSpace(meta.OptionNO)
	if noOption == "" {
		noOption = "NO"
	}

	var description strings.Builder
	fmt.Fprintf(&description, "市场问题：%s\n", title)
	fmt.Fprintf(&description, "客观判定条件：%s\n", strings.TrimSpace(meta.Condition))
	fmt.Fprintf(&description, "YES 选项：%s\n", yesOption)
	fmt.Fprintf(&description, "NO 选项：%s\n", noOption)
	if detail := strings.TrimSpace(meta.DetailedInfo); detail != "" {
		fmt.Fprintf(&description, "补充说明：%s\n", detail)
	}
	if len(meta.AuthoritativeSources) > 0 {
		fmt.Fprintf(&description, "市场创建者指定的权威来源：%s\n", strings.Join(meta.AuthoritativeSources, "、"))
	}
	description.WriteString("裁决映射：仅当客观判定条件在截止时间前得到充分证据确认时 occurred=true（YES）；")
	description.WriteString("仅当充分证据确认条件未满足时 occurred=false（NO）。证据不足时必须降低 confidence，禁止猜测。")

	return aioracle.Event{
		ID:          fmt.Sprintf("game-%d", game.ID),
		Title:       title,
		Description: description.String(),
		Keywords:    eventKeywords(meta),
		Deadline:    deadlineTime(game.DeadlineRaw),
	}
}

func winnerFromVerdict(verdict *aioracle.Verdict) (int, error) {
	if verdict == nil || !verdict.Resolved {
		return -1, fmt.Errorf("AI consensus is indeterminate")
	}
	switch verdict.Decision {
	case aioracle.DecisionYes:
		return 0, nil
	case aioracle.DecisionNo:
		return 1, nil
	default:
		return -1, fmt.Errorf("AI consensus returned unsupported decision %q", verdict.Decision)
	}
}

func deadlineTime(raw int64) time.Time {
	if raw <= 0 {
		return time.Unix(0, 0).UTC()
	}
	if raw <= 10_000_000_000 {
		return time.Unix(raw, 0).UTC()
	}
	return time.UnixMilli(raw).UTC()
}

var keywordTokenPattern = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}._/-]{1,63}`)

func eventKeywords(meta *ipfs.Metadata) []string {
	var candidates []string
	candidates = append(candidates, meta.Keywords...)
	candidates = append(candidates,
		keywordTokenPattern.FindAllString(meta.Desc, -1)...,
	)
	candidates = append(candidates,
		keywordTokenPattern.FindAllString(meta.Condition, -1)...,
	)

	combined := strings.ToLower(meta.Desc + " " + meta.Condition + " " + meta.DetailedInfo)
	if strings.Contains(combined, "黄金") || strings.Contains(combined, "金价") ||
		strings.Contains(combined, "gold") || strings.Contains(combined, "xau") {
		candidates = append(candidates, "黄金", "金价", "gold", "XAU")
	}

	seen := make(map[string]bool)
	keywords := make([]string, 0, 12)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		key := strings.ToLower(candidate)
		if candidate == "" || seen[key] {
			continue
		}
		seen[key] = true
		keywords = append(keywords, candidate)
		if len(keywords) >= 12 {
			break
		}
	}
	return keywords
}

func optionName(meta *ipfs.Metadata, winner int) string {
	if winner == 0 && strings.TrimSpace(meta.OptionYES) != "" {
		return meta.OptionYES
	}
	if winner == 1 && strings.TrimSpace(meta.OptionNO) != "" {
		return meta.OptionNO
	}
	if winner == 0 {
		return "YES"
	}
	return "NO"
}
