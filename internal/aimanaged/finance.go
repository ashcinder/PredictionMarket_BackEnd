package aimanaged

import (
	"math"
	"math/big"
	"time"

	"PredictionMarket/internal/chain"
	"PredictionMarket/internal/ipfs"
	"PredictionMarket/internal/oracle"
)

// PreAnalysis contains pre-computed financial metrics derived from chain data
// and oracle prices, BEFORE the AI is called. These metrics give the AI
// proper financial context to make rational trading decisions.
type PreAnalysis struct {
	// --- Market-implied probabilities (derived from AMM reserves) ---
	//
	// In a constant-product AMM, the price of YES is:
	//   P_YES = R_NO / (R_YES + R_NO)
	// This is the market's consensus probability.

	// MarketProbYES is the market-implied probability of YES winning (0-1).
	MarketProbYES float64 `json:"market_prob_yes"`

	// MarketProbNO is the market-implied probability of NO winning (0-1).
	// Always equals 1 - MarketProbYES.
	MarketProbNO float64 `json:"market_prob_no"`

	// --- Gold price statistics ---

	// GoldPriceUSD is the current gold price.
	GoldPriceUSD float64 `json:"gold_price_usd"`

	// GoldChange24h is the 24-hour percentage change in gold price.
	GoldChange24h float64 `json:"gold_change_24h_pct"`

	// --- Time analysis ---

	// RemainingHours is how many hours until the game deadline.
	RemainingHours float64 `json:"remaining_hours"`

	// TimeDecayFactor is 0 at deadline, 1 when far from deadline.
	// Trades near deadline carry more risk because there's less time
	// for the market to correct.
	TimeDecayFactor float64 `json:"time_decay_factor"`

	// --- Liquidity ---

	// TotalPoolBKC is the total pool value in BKC (converted from wei).
	TotalPoolBKC float64 `json:"total_pool_bkc"`

	// PoolDepthScore rates pool depth: 0 (empty) to 1 (deep).
	// Thin pools are risky because a single trade moves price drastically.
	PoolDepthScore float64 `json:"pool_depth_score"`

	// --- Historical trend ---

	// YesTrendDirection: +1 = uptrend, -1 = downtrend, 0 = flat.
	YesTrendDirection int `json:"yes_trend_direction"`

	// YesTrendStrength is how strong the trend signal is (0-1).
	YesTrendStrength float64 `json:"yes_trend_strength"`

	// VolatilityRecent is the recent price volatility (standard deviation of % changes).
	VolatilityRecent float64 `json:"volatility_recent"`
}

// ComputePreAnalysis derives financial metrics from raw chain + oracle data.
// It runs BEFORE the AI prompt, so the prompt can reference these numbers.
func ComputePreAnalysis(
	extra *chain.GameExtraData,
	quote *oracle.Quote,
	deadlineRaw int64,
	history []ipfs.HistoryPoint,
	now time.Time,
) PreAnalysis {
	pa := PreAnalysis{
		GoldPriceUSD:  quote.PriceUSD,
		GoldChange24h: quote.Change24h,
	}

	// 1. Market-implied probability from AMM reserves.
	if extra != nil && len(extra.VirtualReservesNOYES) >= 2 {
		rNO := float64FromBig(extra.VirtualReservesNOYES[0])
		rYES := float64FromBig(extra.VirtualReservesNOYES[1])
		total := rNO + rYES
		if total > 0 {
			pa.MarketProbYES = rNO / total // Price of YES = NO reserves / total
			pa.MarketProbNO = rYES / total  // Price of NO  = YES reserves / total
		}
		// Pool depth: normalize by a meaningful scale (100 BKC = 100e18 wei).
		// Score rises quickly for small pools then plateaus.
		pa.TotalPoolBKC = total / 1e18
		if pa.TotalPoolBKC > 0 {
			pa.PoolDepthScore = math.Tanh(pa.TotalPoolBKC / 50.0) // tanh(x/50)
		}
	}

	// 2. Time decay.
	remaining := chain.RemainingSecondsUntilDeadline(deadlineRaw, now.UnixMilli())
	if remaining > 0 {
		pa.RemainingHours = float64(remaining) / 3600.0
		// Factor: decays slowly then rapidly near deadline.
		// At 24h: ~0.96, at 1h: ~0.46, at 5min: ~0.04
		pa.TimeDecayFactor = 1.0 - math.Exp(-pa.RemainingHours/4.0)
	}
	if pa.TimeDecayFactor < 0 {
		pa.TimeDecayFactor = 0
	}
	if pa.TimeDecayFactor > 1 {
		pa.TimeDecayFactor = 1
	}

	// 3. Historical trend analysis.
	if len(history) >= 3 {
		pa.YesTrendDirection, pa.YesTrendStrength = computeTrend(history)
		pa.VolatilityRecent = computeVolatility(history)
	}

	return pa
}

// computeTrend determines the direction and strength of the YES price trend.
// Uses simple linear regression over the recent history points.
func computeTrend(history []ipfs.HistoryPoint) (direction int, strength float64) {
	if len(history) < 3 {
		return 0, 0
	}

	// Use last ~70% of available points for trend detection.
	n := len(history)
	start := n / 3
	if n-start < 3 {
		start = 0
	}
	points := history[start:]

	var sumX, sumY, sumXY, sumX2 float64
	m := float64(len(points))
	for i, p := range points {
		x := float64(i)
		y := p.YesPercent
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := m*sumX2 - sumX*sumX
	if math.Abs(denom) < 1e-12 {
		return 0, 0
	}

	slope := (m*sumXY - sumX*sumY) / denom

	// Normalize slope to a -1..1 direction signal.
	// A slope of +1% per tick is very strong; cap at ±2%/tick.
	if slope > 0.05 {
		direction = 1
	} else if slope < -0.05 {
		direction = -1
	}

	// R² as signal strength (how linear is the trend).
	meanY := sumY / m
	var ssReg, ssTot float64
	for i, p := range points {
		predicted := slope*float64(i) + (sumY-slope*sumX)/m
		ssReg += (predicted - meanY) * (predicted - meanY)
		ssTot += (p.YesPercent - meanY) * (p.YesPercent - meanY)
	}
	if ssTot > 1e-12 {
		strength = math.Min(ssReg/ssTot, 1.0)
	}
	if strength < 0 {
		strength = 0
	}
	if math.IsNaN(strength) {
		strength = 0
	}

	return direction, strength
}

// computeVolatility estimates recent price volatility as the average absolute
// tick-to-tick change in YES percentage.
func computeVolatility(history []ipfs.HistoryPoint) float64 {
	if len(history) < 3 {
		return 0
	}
	n := len(history)
	start := n / 3
	if n-start < 2 {
		start = 0
	}

	var sumAbsChange float64
	count := 0
	for i := start + 1; i < n; i++ {
		change := math.Abs(history[i].YesPercent - history[i-1].YesPercent)
		sumAbsChange += change
		count++
	}
	if count == 0 {
		return 0
	}
	return sumAbsChange / float64(count)
}

// =============================================================================
// Kelly Criterion Position Sizing
// =============================================================================

// KellyResult holds the output of the Kelly criterion calculation.
type KellyResult struct {
	// ShouldBet is true if the edge is large enough to justify a bet.
	ShouldBet bool `json:"should_bet"`

	// Direction: 0 = YES, 1 = NO, -1 = no bet.
	Direction int `json:"direction"`

	// KellyFraction is the full Kelly fraction (0-1). This is the theoretical
	// optimal bet size as a fraction of bankroll. In practice we use a
	// fractional Kelly to be conservative.
	KellyFraction float64 `json:"kelly_fraction"`

	// EdgePercent is the absolute difference between estimated and market probability.
	EdgePercent float64 `json:"edge_percent"`

	// EstimatedProb is the AI's estimated true probability.
	EstimatedProb float64 `json:"estimated_prob"`

	// MarketProb is the market-implied probability for the chosen direction.
	MarketProb float64 `json:"market_prob"`

	// BetSizeFactor is 0-1, scaling the base bet amount. Multiply baseAmount
	// by this to get the actual bet size.
	BetSizeFactor float64 `json:"bet_size_factor"`
}

// ComputeKelly calculates the optimal bet size using the Kelly criterion
// adapted for prediction markets with constant-product AMM.
//
// For a binary prediction market:
//   - Market price = P (implied probability of chosen outcome)
//   - Our estimate  = p (true probability, from AI)
//   - Edge = p - P (how much we think the market is wrong)
//
// Full Kelly fraction:
//
//	If betting YES:  f* = (p - P) / (1 - P)   when p > P
//	If betting NO:   f* = (P - p) / P          when p < P
//
// We use fractional Kelly (default 1/4) for safety, and apply additional
// dampening from time decay and pool depth.
func ComputeKelly(
	estimatedProb float64, // AI's estimated true probability of YES (0-1)
	pre PreAnalysis,
	fractionalKelly float64, // e.g., 0.25 for quarter-Kelly
	minEdgePercent float64, // minimum edge to trade, e.g., 0.05 (5%)
) KellyResult {
	kr := KellyResult{
		EstimatedProb: estimatedProb,
		Direction:     -1,
	}

	// Clamp inputs.
	estimatedProb = clamp01(estimatedProb)
	marketProb := clamp01(pre.MarketProbYES)

	kr.MarketProb = marketProb

	// Determine direction and compute edge.
	var edge float64
	if estimatedProb > marketProb {
		kr.Direction = 0 // YES
		edge = estimatedProb - marketProb
		kr.MarketProb = marketProb
		// Kelly for betting on an outcome: f* = edge / (1 - market_prob)
		if marketProb < 1.0 {
			kr.KellyFraction = edge / (1.0 - marketProb)
		}
	} else {
		kr.Direction = 1 // NO
		edge = marketProb - estimatedProb
		kr.MarketProb = 1.0 - marketProb
		// Kelly for betting against: f* = edge / market_prob
		if marketProb > 0 {
			kr.KellyFraction = edge / marketProb
		}
	}

	kr.EdgePercent = edge * 100

	// Check minimum edge threshold.
	if edge < minEdgePercent/100.0 {
		return kr // ShouldBet remains false
	}

	kr.ShouldBet = true

	// Apply fractional Kelly for safety.
	safeFraction := kr.KellyFraction * fractionalKelly
	if safeFraction > 1.0 {
		safeFraction = 1.0
	}
	if safeFraction < 0 {
		safeFraction = 0
	}

	// Dampen by time decay: bet less as deadline approaches.
	// Near deadline, market prices become more sticky and risk rises.
	safeFraction *= pre.TimeDecayFactor

	// Dampen by pool depth: bet less in thin pools.
	// Thin pools have high price impact per unit of trade.
	if pre.PoolDepthScore < 0.3 {
		safeFraction *= pre.PoolDepthScore / 0.3 // Linear reduction below score 0.3
	}

	kr.BetSizeFactor = clamp01(safeFraction)
	return kr
}

// =============================================================================
// Adaptive Cooldown
// =============================================================================

// AdaptiveCooldownSeconds computes how many seconds to wait before the next
// trade, based on market conditions. This replaces the fixed 1-hour cooldown.
//
// Logic:
//   - Base cooldown: 30 minutes
//   - Large edge → shorten cooldown (we want to act fast on good opportunities)
//   - Small edge → lengthen cooldown (don't overtrade on noise)
//   - Near deadline → shorten cooldown (fewer chances left)
//   - High volatility → lengthen cooldown (wait for dust to settle)
func AdaptiveCooldownSeconds(edgePercent float64, remainingHours float64, volatility float64) int64 {
	base := 1800.0 // 30 minutes base

	// Edge adjustment: large edge → faster re-entry.
	// edgePercent=5% → multiplier≈1.0, edgePercent=20% → multiplier≈0.4
	edgeMult := 1.0
	if edgePercent > 5.0 {
		edgeMult = 5.0 / edgePercent
	}
	if edgeMult < 0.2 {
		edgeMult = 0.2 // Minimum 6 minutes
	}

	// Time adjustment: near deadline → faster re-entry.
	timeMult := 1.0
	if remainingHours < 6 {
		timeMult = remainingHours / 6.0
	}
	if timeMult < 0.1 {
		timeMult = 0.1 // Minimum 3 minutes
	}

	// Volatility adjustment: high vol → longer cooldown.
	volMult := 1.0
	if volatility > 2.0 { // >2% per tick is high
		volMult = volatility / 2.0
	}
	if volMult > 3.0 {
		volMult = 3.0
	}

	cooldown := base * edgeMult * timeMult * volMult
	if cooldown < 180 { // Never shorter than 3 minutes
		cooldown = 180
	}
	if cooldown > 7200 { // Never longer than 2 hours
		cooldown = 7200
	}

	return int64(cooldown)
}

// =============================================================================
// Helpers
// =============================================================================

func float64FromBig(b *big.Int) float64 {
	if b == nil {
		return 0
	}
	f := new(big.Float).SetInt(b)
	v, _ := f.Float64()
	return v
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	if math.IsNaN(v) {
		return 0
	}
	return v
}
