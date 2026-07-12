package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"PredictionMarket/internal/judge"
)

type GoldAPIClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewGoldAPIClient(baseURL, apiKey string, timeout time.Duration) *GoldAPIClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &GoldAPIClient{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: strings.TrimSpace(apiKey),
		client: &http.Client{Timeout: timeout},
	}
}

func (c *GoldAPIClient) Available() bool { return c != nil && c.baseURL != "" && c.apiKey != "" }

func (c *GoldAPIClient) OHLC(ctx context.Context, symbol string, start, end time.Time) ([]judge.Candle, error) {
	if !c.Available() {
		return nil, fmt.Errorf("historical Gold API is not configured")
	}
	values := url.Values{
		"startTimestamp": {strconv.FormatInt(start.Unix(), 10)},
		"endTimestamp":   {strconv.FormatInt(end.Unix(), 10)},
	}
	var payload struct {
		Open  float64 `json:"open"`
		High  float64 `json:"high"`
		Low   float64 `json:"low"`
		Close float64 `json:"close"`
	}
	if err := c.getJSON(ctx, "/ohlc/"+url.PathEscape(strings.ToUpper(symbol)), values, &payload); err != nil {
		return nil, err
	}
	return []judge.Candle{{Time: start, Open: payload.Open, High: payload.High, Low: payload.Low, Close: payload.Close}}, nil
}

// History returns ordered average-price observations. Minute/hour grouping
// requires the corresponding Gold API plan; callers fail closed on errors.
func (c *GoldAPIClient) History(ctx context.Context, symbol, groupBy string, start, end time.Time) ([]judge.Candle, error) {
	if !c.Available() {
		return nil, fmt.Errorf("historical Gold API is not configured")
	}
	values := url.Values{
		"symbol":         {strings.ToUpper(symbol)},
		"startTimestamp": {strconv.FormatInt(start.Unix(), 10)},
		"endTimestamp":   {strconv.FormatInt(end.Unix(), 10)},
		"groupBy":        {groupBy},
		"aggregation":    {"avg"},
		"orderBy":        {"asc"},
	}
	var raw []map[string]interface{}
	if err := c.getJSON(ctx, "/history", values, &raw); err != nil {
		return nil, err
	}
	candles := make([]judge.Candle, 0, len(raw))
	for _, item := range raw {
		stamp, err := historyTime(item, groupBy)
		if err != nil {
			return nil, err
		}
		price, ok := numberField(item, "avg_price")
		if !ok || price <= 0 {
			return nil, fmt.Errorf("Gold API history row has invalid avg_price")
		}
		candles = append(candles, judge.Candle{Time: stamp, Open: price, High: price, Low: price, Close: price})
	}
	return candles, nil
}

func (c *GoldAPIClient) getJSON(ctx context.Context, path string, values url.Values, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("Gold API request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Gold API %s returned HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Gold API %s: %w", path, err)
	}
	return nil
}

func historyTime(item map[string]interface{}, groupBy string) (time.Time, error) {
	value, _ := item[groupBy].(string)
	formats := []string{"2006-01-02 15:04", "2006-01-02 15", "2006-01-02"}
	for _, format := range formats {
		if parsed, err := time.ParseInLocation(format, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("Gold API history row has invalid %s time %q", groupBy, value)
}

func numberField(item map[string]interface{}, name string) (float64, bool) {
	switch value := item[name].(type) {
	case float64:
		return value, true
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
