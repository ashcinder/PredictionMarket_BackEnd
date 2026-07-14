package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"PredictionMarket/internal/judge"
)

// CoinbaseClient reads public one-minute BTC-USD candles for relative-return
// markets. The returned candle is normalized to the market's committed window
// so the deterministic evaluator can compare it with the XAU window.
type CoinbaseClient struct {
	baseURL string
	client  *http.Client
}

func NewCoinbaseClient(baseURL string, timeout time.Duration) *CoinbaseClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &CoinbaseClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *CoinbaseClient) Available() bool { return c != nil && c.baseURL != "" }

func (c *CoinbaseClient) OHLC(ctx context.Context, symbol string, start, end time.Time) ([]judge.Candle, error) {
	if !c.Available() {
		return nil, fmt.Errorf("Coinbase historical API is not configured")
	}
	if !strings.EqualFold(strings.TrimSpace(symbol), "BTC") &&
		!strings.EqualFold(strings.TrimSpace(symbol), "BTC-USD") {
		return nil, fmt.Errorf("Coinbase benchmark %q is not supported", symbol)
	}
	if !end.After(start) {
		return nil, fmt.Errorf("Coinbase observation window is invalid")
	}
	values := url.Values{
		"start":       {start.UTC().Format(time.RFC3339)},
		"end":         {end.UTC().Format(time.RFC3339)},
		"granularity": {"60"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/products/BTC-USD/candles?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PredictionMarket/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Coinbase candle request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Coinbase candle API returned HTTP %d", resp.StatusCode)
	}
	var rows [][]float64
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode Coinbase candles: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("Coinbase returned no BTC-USD candles")
	}
	for _, row := range rows {
		if len(row) < 6 {
			return nil, fmt.Errorf("Coinbase returned a malformed candle")
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	open := rows[0][3]
	closePrice := rows[len(rows)-1][4]
	high, low, volume := -math.MaxFloat64, math.MaxFloat64, 0.0
	for _, row := range rows {
		low = math.Min(low, row[1])
		high = math.Max(high, row[2])
		volume += row[5]
	}
	if open <= 0 || closePrice <= 0 || high <= 0 || low <= 0 || high < low {
		return nil, fmt.Errorf("Coinbase returned invalid BTC-USD prices")
	}
	return []judge.Candle{{
		Time: start.UTC(), Open: open, High: high, Low: low, Close: closePrice, Volume: volume,
	}}, nil
}
