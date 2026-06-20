package judge

import (
	"math"
	"strconv"
	"strings"

	"PredictionMarket/internal/oracle"
)

// EvaluateWinner mirrors agent/gold/model/logic/GoldGameJudge.java.
// Returns winning option index: 0 = YES, 1 = NO, -1 = cannot evaluate.
func EvaluateWinner(condition string, quote *oracle.Quote) int {
	if strings.TrimSpace(condition) == "" || quote == nil {
		return -1
	}
	cond := condition

	if strings.Contains(cond, "相对基准") {
		if strings.Contains(cond, "上涨") {
			if quote.Change24h > 0 {
				return 0
			}
			return 1
		}
		if strings.Contains(cond, "下跌") {
			if quote.Change24h < 0 {
				return 0
			}
			return 1
		}
		if strings.Contains(cond, "持平") {
			if math.Abs(quote.Change24h) < 0.01 {
				return 0
			}
			return 1
		}
	}

	if strings.Contains(cond, "波幅 >= ") {
		if threshold, ok := parseNumericValue(cond, "波幅 >= ", "%"); ok {
			if math.Abs(quote.Change24h) >= threshold {
				return 0
			}
			return 1
		}
	}

	if strings.Contains(cond, "成交量") {
		if targetVol, ok := parseNumericValue(cond, "成交量 ", " 吨"); ok {
			actualVol := 400.0
			if strings.Contains(cond, "大于") {
				if actualVol > targetVol {
					return 0
				}
				return 1
			}
			if strings.Contains(cond, "小于") {
				if actualVol < targetVol {
					return 0
				}
				return 1
			}
			if strings.Contains(cond, "等于") {
				if math.Abs(actualVol-targetVol) < 1.0 {
					return 0
				}
				return 1
			}
		}
	}

	if strings.Contains(cond, "指标") {
		if idx := strings.Index(cond, " ("); idx > 0 {
			lastSpace := strings.LastIndex(cond[:idx], " ")
			if lastSpace >= 0 && lastSpace+1 < idx {
				valueStr := strings.TrimSpace(cond[lastSpace+1 : idx])
				if threshold, err := strconv.ParseFloat(valueStr, 64); err == nil {
					currentIndicatorValue := 65.0
					if strings.Contains(cond, "大于") {
						if currentIndicatorValue > threshold {
							return 0
						}
						return 1
					}
					if strings.Contains(cond, "小于") {
						if currentIndicatorValue < threshold {
							return 0
						}
						return 1
					}
				}
			}
		}
	}

	if strings.Contains(cond, "曾触及") {
		if target, ok := parseNumericValue(cond, "触及 ", " USD"); ok {
			if quote.PriceUSD >= target {
				return 0
			}
			return 1
		}
	}

	if strings.Contains(cond, "跑赢") {
		return 0
	}

	return 1
}

func parseNumericValue(text, prefix, suffix string) (float64, bool) {
	start := strings.Index(text, prefix)
	if start < 0 {
		return 0, false
	}
	start += len(prefix)
	end := strings.Index(text[start:], suffix)
	if end < 0 {
		return 0, false
	}
	val := strings.TrimSpace(text[start : start+end])
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func OptionName(optionNames []string, winner int) string {
	if len(optionNames) > winner && optionNames[winner] != "" {
		return optionNames[winner]
	}
	if winner == 0 {
		return "YES"
	}
	return "NO"
}
