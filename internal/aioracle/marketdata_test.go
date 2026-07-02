package aioracle

import "testing"

func TestContainsGoldKeyword(t *testing.T) {
	for _, keywords := range [][]string{
		{"黄金价格"},
		{"XAU/USD"},
		{"gold price"},
		{"今日金价"},
	} {
		if !containsGoldKeyword(keywords) {
			t.Errorf("expected gold keywords to match: %v", keywords)
		}
	}
	if containsGoldKeyword([]string{"bitcoin", "BTC"}) {
		t.Error("non-gold market should not request gold evidence")
	}
}
