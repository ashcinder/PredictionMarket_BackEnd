package judge

import (
	"math"
	"regexp"
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

	// ============================================================
	// 模版 1: 价格涨跌 (Binary Price Option)
	// 格式: "博弈黄金价格在 ... 相对基准 上涨 (Price Up)"
	// ============================================================
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

	// ============================================================
	// 模版 2: 波动幅度 (Volatility Spread)
	// 格式: "博弈周期内波幅 >= 5% (...)"
	// ============================================================
	if strings.Contains(cond, "波幅") {
		if threshold, ok := parseNumericValue(cond, "波幅 >= ", "%"); ok {
			if math.Abs(quote.Change24h) >= threshold {
				return 0
			}
			return 1
		}
		if threshold, ok := parseNumericValue(cond, "波幅 > ", "%"); ok {
			if math.Abs(quote.Change24h) > threshold {
				return 0
			}
			return 1
		}
		if threshold, ok := parseNumericValue(cond, "波幅 <= ", "%"); ok {
			if math.Abs(quote.Change24h) <= threshold {
				return 0
			}
			return 1
		}
		if threshold, ok := parseNumericValue(cond, "波幅 < ", "%"); ok {
			if math.Abs(quote.Change24h) < threshold {
				return 0
			}
			return 1
		}
	}

	// ============================================================
	// 模版 3: 交易量 (Liquidity Volume)
	// 格式: "博弈指定日成交量 大于 (Above) 500 吨 (...)"
	// ============================================================
	if strings.Contains(cond, "成交量") {
		if targetVol, ok := parseComparisonThreshold(cond); ok {
			actualVol := 400.0 // TODO: 这里需要对接真实的交易量数据源
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

	// ============================================================
	// 模版 4: 技术指标 (Technical Indicators)
	// 格式: "指标 RSI (14) 大于 (Above) 70 (...)"
	// 格式: "指标 MACD 交叉向上 (...)"
	// ============================================================
	if strings.Contains(cond, "指标") {
		// 先尝试解析 "大于/小于" 模式
		if threshold, ok := parseComparisonThreshold(cond); ok {
			currentIndicatorValue := 65.0 // TODO: 这里需要计算真实的技术指标

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

		// 解析 "交叉向上/交叉向下" 模式
		if strings.Contains(cond, "交叉") {
			if strings.Contains(cond, "交叉向上") {
				return 0 // TODO: 需要历史数据才能判断交叉
			}
			if strings.Contains(cond, "交叉向下") {
				return 1 // TODO: 需要历史数据才能判断交叉
			}
		}
	}

	// ============================================================
	// 模版 5: 极值碰触 (One-Touch Barrier)
	// 格式: "金价曾触及 2500 USD (...)"
	// ============================================================
	if strings.Contains(cond, "曾触及") || strings.Contains(cond, "触及") {
		if target, ok := parseNumericValue(cond, "触及 ", " USD"); ok {
			if quote.PriceUSD >= target {
				return 0
			}
			return 1
		}
	}

	// ============================================================
	// 模版 6: 跑赢率 (Outperformance)
	// 格式: "黄金收益率跑赢 BTC (...)"
	// ============================================================
	if strings.Contains(cond, "跑赢") {
		// TODO: 这里需要获取对标资产的收益率
		// 当前逻辑：暂时默认返回 YES（与 Android 保持一致）
		return 0
	}

	// 默认返回 NO
	return 1
}

var comparisonThresholdPattern = regexp.MustCompile(`(?:大于|小于|等于)\s*(?:\([^)]*\))?\s*(-?\d+(?:\.\d+)?)`)

func parseComparisonThreshold(condition string) (float64, bool) {
	match := comparisonThresholdPattern.FindStringSubmatch(condition)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return value, true
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
