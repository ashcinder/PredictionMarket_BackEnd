package judge

import (
	"testing"

	"PredictionMarket/internal/oracle"
)

func TestEvaluateWinner_Template1_Price(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		quote    *oracle.Quote
		expected int
	}{
		// 模板1: 价格涨跌
		{
			name:     "价格上涨 - 满足条件",
			cond:     "博弈黄金价格在 截至 2026-06-15 相对基准 上涨 (Price Up)",
			quote:    &oracle.Quote{PriceUSD: 4150, Change24h: 1.5},
			expected: 0,
		},
		{
			name:     "价格上涨 - 不满足条件",
			cond:     "博弈黄金价格在 截至 2026-06-15 相对基准 上涨 (Price Up)",
			quote:    &oracle.Quote{PriceUSD: 4150, Change24h: -0.5},
			expected: 1,
		},
		{
			name:     "价格下跌 - 满足条件",
			cond:     "博弈黄金价格在 截至 2026-06-15 相对基准 下跌 (Price Down)",
			quote:    &oracle.Quote{PriceUSD: 4150, Change24h: -0.5},
			expected: 0,
		},
		{
			name:     "价格持平 - 满足条件",
			cond:     "博弈黄金价格在 截至 2026-06-15 相对基准 持平 (Price Flat)",
			quote:    &oracle.Quote{PriceUSD: 4150, Change24h: 0.005},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateWinner(tt.cond, tt.quote)
			if result != tt.expected {
				t.Errorf("EvaluateWinner() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEvaluateWinner_Template2_Volatility(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		quote    *oracle.Quote
		expected int
	}{
		// 模板2: 波动幅度
		{
			name:     "波幅 >= 5% - 满足",
			cond:     "博弈周期内波幅 >= 5% (Volatility Option)",
			quote:    &oracle.Quote{PriceUSD: 4150, Change24h: 6.0},
			expected: 0,
		},
		{
			name:     "波幅 >= 5% - 不满足",
			cond:     "博弈周期内波幅 >= 5% (Volatility Option)",
			quote:    &oracle.Quote{PriceUSD: 4150, Change24h: 3.0},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateWinner(tt.cond, tt.quote)
			if result != tt.expected {
				t.Errorf("EvaluateWinner() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEvaluateWinner_Template3_Volume(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		quote    *oracle.Quote
		expected int
	}{
		// 模板3: 交易量
		{
			name:     "成交量大于100吨 - 满足（当前模拟400）",
			cond:     "博弈指定日成交量 大于 (Above) 100 吨 (2026-06-15)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 0,
		},
		{
			name:     "成交量大于500吨 - 不满足",
			cond:     "博弈指定日成交量 大于 (Above) 500 吨 (2026-06-15)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateWinner(tt.cond, tt.quote)
			if result != tt.expected {
				t.Errorf("EvaluateWinner() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEvaluateWinner_Template4_Indicator(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		quote    *oracle.Quote
		expected int
	}{
		// 模板4: 技术指标
		{
			name:     "指标RSI大于70 - 当前65不满足",
			cond:     "指标 RSI (14) 大于 (Above) 70 (Indicator Option)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 1,
		},
		{
			name:     "指标RSI大于60 - 当前65满足",
			cond:     "指标 RSI (14) 大于 (Above) 60 (Indicator Option)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 0,
		},
		{
			name:     "指标MACD交叉向上",
			cond:     "指标 MACD 交叉向上 (Indicator Option)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateWinner(tt.cond, tt.quote)
			if result != tt.expected {
				t.Errorf("EvaluateWinner() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEvaluateWinner_Template5_Touch(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		quote    *oracle.Quote
		expected int
	}{
		// 模板5: 极值碰触
		{
			name:     "触及4000 USD - 满足",
			cond:     "金价曾触及 4000 USD (One-Touch Option)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 0,
		},
		{
			name:     "触及4500 USD - 不满足",
			cond:     "金价曾触及 4500 USD (One-Touch Option)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateWinner(tt.cond, tt.quote)
			if result != tt.expected {
				t.Errorf("EvaluateWinner() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEvaluateWinner_Template6_Outperform(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		quote    *oracle.Quote
		expected int
	}{
		// 模板6: 跑赢率
		{
			name:     "黄金跑赢BTC",
			cond:     "黄金收益率跑赢 BTC (Outperformance Option)",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateWinner(tt.cond, tt.quote)
			if result != tt.expected {
				t.Errorf("EvaluateWinner() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEvaluateWinner_InvalidCases(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		quote    *oracle.Quote
		expected int
	}{
		{
			name:     "空条件",
			cond:     "",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: -1,
		},
		{
			name:     "nil quote",
			cond:     "价格上涨",
			quote:    nil,
			expected: -1,
		},
		{
			name:     "未知条件",
			cond:     "未知条件",
			quote:    &oracle.Quote{PriceUSD: 4150},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluateWinner(tt.cond, tt.quote)
			if result != tt.expected {
				t.Errorf("EvaluateWinner() = %v, want %v", result, tt.expected)
			}
		})
	}
}
