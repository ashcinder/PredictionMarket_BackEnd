package aicenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"PredictionMarket/internal/aioracle"
	appcache "PredictionMarket/internal/cache"
	"PredictionMarket/internal/sentinel"
)

const (
	overviewCacheTTL = 15 * time.Second
	queryTimeout     = 8 * time.Second
)

// Repository is both the read model for the mobile decision center and the
// append-only audit sink used by Sentinel. MySQL remains the source of truth.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

type cacheInfo struct {
	Enabled bool   `json:"enabled"`
	Hit     bool   `json:"hit"`
	Mode    string `json:"mode"`
	TTL     int64  `json:"ttl_seconds"`
}

type Stats struct {
	ManagedMarkets  int `json:"managed_markets"`
	RecentDecisions int `json:"recent_decisions"`
	SettledAudits   int `json:"settled_audits"`
	Opportunities   int `json:"opportunities"`
}

type ManagedDecision struct {
	ID                 int64   `json:"id"`
	GameID             int     `json:"game_id"`
	ContractAddress    string  `json:"contract_address"`
	MarketTitle        string  `json:"market_title"`
	ObservedAt         int64   `json:"observed_at"`
	Action             string  `json:"action"`
	Confidence         float64 `json:"confidence"`
	EstimatedProbYES   float64 `json:"estimated_prob_yes"`
	MarketProbYES      float64 `json:"market_prob_yes"`
	ProbabilityEdgePct float64 `json:"probability_edge_percent"`
	Reason             string  `json:"reason"`
	HistoryPoints      int     `json:"history_points"`
	Outcome            string  `json:"outcome"`
	TxHash             string  `json:"tx_hash"`
	ErrorSummary       string  `json:"error_summary"`
}

type SettlementAudit struct {
	ID                     int64                   `json:"id"`
	GameID                 int                     `json:"game_id"`
	ContractAddress        string                  `json:"contract_address"`
	MarketTitle            string                  `json:"market_title"`
	RuleSummary            string                  `json:"rule_summary"`
	DeterministicCandidate string                  `json:"deterministic_candidate"`
	Evidence               []aioracle.NewsArticle  `json:"evidence"`
	Opinions               []aioracle.ModelOpinion `json:"opinions"`
	FinalDecision          string                  `json:"final_decision"`
	FinalConfidence        float64                 `json:"final_confidence"`
	ConsensusRatio         float64                 `json:"consensus_ratio"`
	FinalSummary           string                  `json:"final_summary"`
	ResolvedAt             time.Time               `json:"resolved_at"`
}

type Opportunity struct {
	DecisionID      int64   `json:"decision_id"`
	GameID          int     `json:"game_id"`
	ContractAddress string  `json:"contract_address"`
	MarketTitle     string  `json:"market_title"`
	Side            string  `json:"side"`
	Confidence      float64 `json:"confidence"`
	EstimatedProb   float64 `json:"estimated_probability"`
	MarketProb      float64 `json:"market_probability"`
	EdgePercent     float64 `json:"edge_percent"`
	LiquidityBKC    float64 `json:"liquidity_bkc"`
	DeadlineSec     int64   `json:"deadline_sec"`
	Reason          string  `json:"reason"`
	ObservedAt      int64   `json:"observed_at"`
}

type Overview struct {
	GeneratedAt      time.Time         `json:"generated_at"`
	Cache            cacheInfo         `json:"cache"`
	Stats            Stats             `json:"stats"`
	ManagedDecisions []ManagedDecision `json:"managed_decisions"`
	SettlementAudits []SettlementAudit `json:"settlement_audits"`
	Opportunities    []Opportunity     `json:"opportunities"`
}

type Server struct {
	repo  *Repository
	cache appcache.Store
}

func NewServer(repo *Repository, cache appcache.Store) *Server {
	return &Server{repo: repo, cache: cache}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/gold/ai-center", s.handleOverview)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	user := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("user_address")))
	cacheKey := "ai-center:v1:" + url.QueryEscape(user)
	if s.cache != nil {
		if raw, ok, err := s.cache.Get(r.Context(), cacheKey); err == nil && ok {
			var overview Overview
			if json.Unmarshal(raw, &overview) == nil {
				overview.Cache = cacheInfo{Enabled: true, Hit: true, Mode: "redis", TTL: int64(overviewCacheTTL.Seconds())}
				writeJSON(w, http.StatusOK, overview)
				return
			}
		}
	}
	overview, err := s.repo.Overview(r.Context(), user)
	if err != nil {
		http.Error(w, `{"error":"AI 决策中心暂时不可用"}`, http.StatusInternalServerError)
		slog.Warn("load AI decision center failed", "user", user, "error", err)
		return
	}
	overview.Cache = cacheInfo{
		Enabled: s.cache != nil, Hit: false,
		Mode: cacheMode(s.cache), TTL: int64(overviewCacheTTL.Seconds()),
	}
	if s.cache != nil {
		if raw, err := json.Marshal(overview); err == nil {
			if err := s.cache.Set(r.Context(), cacheKey, raw, overviewCacheTTL); err != nil {
				slog.Debug("cache AI decision center failed", "error", err)
			}
		}
	}
	writeJSON(w, http.StatusOK, overview)
}

func cacheMode(store appcache.Store) string {
	if store == nil {
		return "database"
	}
	return "redis"
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (r *Repository) Overview(ctx context.Context, user string) (Overview, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	managed, err := r.listManagedDecisions(ctx, user)
	if err != nil {
		return Overview{}, err
	}
	settlements, err := r.listSettlements(ctx)
	if err != nil {
		return Overview{}, err
	}
	opportunities := buildOpportunities(managed)
	stats, err := r.stats(ctx, user, len(settlements), len(opportunities))
	if err != nil {
		return Overview{}, err
	}
	return Overview{
		GeneratedAt: time.Now().UTC(), Stats: stats,
		ManagedDecisions: managed, SettlementAudits: settlements,
		Opportunities: opportunities,
	}, nil
}

func (r *Repository) listManagedDecisions(ctx context.Context, user string) ([]ManagedDecision, error) {
	query := `SELECT d.id, d.game_id, d.contract_address,
COALESCE(NULLIF(g.desc,''), NULLIF(g.condition,''), CONCAT('博弈池 #', d.game_id)),
d.observed_at, d.action, CAST(d.confidence AS CHAR), d.reason, d.history_points,
d.outcome, d.tx_hash, d.error_summary,
COALESCE(CAST(cs.reserve_yes AS CHAR),''), COALESCE(CAST(cs.reserve_no AS CHAR),'')
FROM ai_decisions d
LEFT JOIN gold_games g ON g.contract_address=d.contract_address AND g.game_id=d.game_id
LEFT JOIN gold_chain_states cs ON cs.contract_address=d.contract_address AND cs.game_id=d.game_id`
	args := make([]any, 0, 2)
	if user != "" {
		query += " WHERE d.user_address=?"
		args = append(args, user)
	}
	query += " ORDER BY d.observed_at DESC, d.id DESC LIMIT 30"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query managed decisions: %w", err)
	}
	defer rows.Close()
	result := make([]ManagedDecision, 0, 30)
	for rows.Next() {
		var item ManagedDecision
		var confidence, reserveYES, reserveNO string
		if err := rows.Scan(
			&item.ID, &item.GameID, &item.ContractAddress, &item.MarketTitle,
			&item.ObservedAt, &item.Action, &confidence, &item.Reason,
			&item.HistoryPoints, &item.Outcome, &item.TxHash, &item.ErrorSummary,
			&reserveYES, &reserveNO,
		); err != nil {
			return nil, fmt.Errorf("scan managed decision: %w", err)
		}
		item.Confidence, _ = strconv.ParseFloat(confidence, 64)
		item.EstimatedProbYES, item.MarketProbYES = probabilitiesFromReason(item.Reason)
		if item.MarketProbYES <= 0 {
			item.MarketProbYES = probabilityFromReserves(reserveYES, reserveNO)
		}
		item.ProbabilityEdgePct = (item.EstimatedProbYES - item.MarketProbYES) * 100
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) listSettlements(ctx context.Context) ([]SettlementAudit, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, game_id, contract_address, market_title,
rule_summary, deterministic_candidate, COALESCE(CAST(evidence_json AS CHAR),'[]'),
COALESCE(CAST(opinions_json AS CHAR),'[]'), final_decision,
CAST(final_confidence AS CHAR), CAST(consensus_ratio AS CHAR), final_summary, resolved_at
FROM ai_settlement_audits ORDER BY resolved_at DESC, id DESC LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("query settlement audits: %w", err)
	}
	defer rows.Close()
	result := make([]SettlementAudit, 0, 20)
	for rows.Next() {
		var item SettlementAudit
		var evidenceJSON, opinionsJSON, confidence, ratio string
		if err := rows.Scan(
			&item.ID, &item.GameID, &item.ContractAddress, &item.MarketTitle,
			&item.RuleSummary, &item.DeterministicCandidate, &evidenceJSON,
			&opinionsJSON, &item.FinalDecision, &confidence, &ratio,
			&item.FinalSummary, &item.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scan settlement audit: %w", err)
		}
		item.FinalConfidence, _ = strconv.ParseFloat(confidence, 64)
		item.ConsensusRatio, _ = strconv.ParseFloat(ratio, 64)
		_ = json.Unmarshal([]byte(evidenceJSON), &item.Evidence)
		_ = json.Unmarshal([]byte(opinionsJSON), &item.Opinions)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) stats(ctx context.Context, user string, settlements, opportunities int) (Stats, error) {
	var stats Stats
	managedQuery := "SELECT COUNT(*) FROM ai_managed_entries"
	decisionQuery := "SELECT COUNT(*) FROM ai_decisions WHERE observed_at>=?"
	args := []any{time.Now().Add(-24 * time.Hour).Unix()}
	if user != "" {
		managedQuery += " WHERE user_address=?"
		decisionQuery += " AND user_address=?"
	}
	if user == "" {
		if err := r.db.QueryRowContext(ctx, managedQuery).Scan(&stats.ManagedMarkets); err != nil {
			return stats, err
		}
		if err := r.db.QueryRowContext(ctx, decisionQuery, args...).Scan(&stats.RecentDecisions); err != nil {
			return stats, err
		}
	} else {
		if err := r.db.QueryRowContext(ctx, managedQuery, user).Scan(&stats.ManagedMarkets); err != nil {
			return stats, err
		}
		args = append(args, user)
		if err := r.db.QueryRowContext(ctx, decisionQuery, args...).Scan(&stats.RecentDecisions); err != nil {
			return stats, err
		}
	}
	stats.SettledAudits = settlements
	stats.Opportunities = opportunities
	return stats, nil
}

func probabilitiesFromReason(reason string) (estimated, market float64) {
	for _, field := range strings.Fields(reason) {
		keyValue := strings.SplitN(strings.Trim(field, " ,;"), "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		value, err := strconv.ParseFloat(strings.Trim(keyValue[1], " ,;"), 64)
		if err != nil {
			continue
		}
		switch keyValue[0] {
		case "est_prob":
			estimated = value
		case "market_prob":
			market = value
		}
	}
	return estimated, market
}

func probabilityFromReserves(reserveYES, reserveNO string) float64 {
	yes, errYES := strconv.ParseFloat(reserveYES, 64)
	no, errNO := strconv.ParseFloat(reserveNO, 64)
	if errYES != nil || errNO != nil || yes+no <= 0 {
		return 0
	}
	return no / (yes + no)
}

func buildOpportunities(decisions []ManagedDecision) []Opportunity {
	seen := make(map[string]bool)
	result := make([]Opportunity, 0, 8)
	for _, item := range decisions {
		key := strings.ToLower(item.ContractAddress) + ":" + strconv.Itoa(item.GameID)
		if seen[key] {
			continue
		}
		seen[key] = true
		if item.Confidence < .70 || (item.Action != "buy_yes" && item.Action != "buy_no") {
			continue
		}
		if item.Outcome == "low_confidence" || item.Outcome == "hold" {
			continue
		}
		side := "YES"
		estimated, market := item.EstimatedProbYES, item.MarketProbYES
		edge := item.ProbabilityEdgePct
		if item.Action == "buy_no" {
			side = "NO"
			estimated, market = 1-estimated, 1-market
			edge = -edge
		}
		if edge < 5 {
			continue
		}
		result = append(result, Opportunity{
			DecisionID: item.ID, GameID: item.GameID, ContractAddress: item.ContractAddress,
			MarketTitle: item.MarketTitle, Side: side, Confidence: item.Confidence,
			EstimatedProb: estimated, MarketProb: market, EdgePercent: edge,
			Reason: item.Reason, ObservedAt: item.ObservedAt,
		})
		if len(result) == 8 {
			break
		}
	}
	return result
}

// RecordSettlementAudit persists the exact evidence and model outputs produced
// by Sentinel. It never synthesizes missing model opinions.
func (r *Repository) RecordSettlementAudit(ctx context.Context, record sentinel.SettlementAuditRecord) error {
	evidenceJSON, err := json.Marshal(record.Evidence)
	if err != nil {
		return err
	}
	opinionsJSON, err := json.Marshal(record.Verdict.Opinions)
	if err != nil {
		return err
	}
	resolvedAt := record.Verdict.ResolvedAt
	if resolvedAt.IsZero() {
		resolvedAt = time.Now().UTC()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO ai_settlement_audits
(contract_address, game_id, market_title, rule_summary, deterministic_candidate,
evidence_json, opinions_json, final_decision, final_confidence, consensus_ratio,
final_summary, resolved_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.ToLower(record.ContractAddress), record.GameID, record.MarketTitle,
		record.RuleSummary, record.DeterministicCandidate, evidenceJSON, opinionsJSON,
		string(record.Verdict.Decision), record.Verdict.Confidence,
		record.Verdict.ConsensusRatio, record.Verdict.Summary, resolvedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert settlement audit: %w", err)
	}
	return nil
}
