package oracle

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Quote struct {
	PriceUSD      float64
	Change24h     float64
	QuoteSource   string
	QuoteUpdatedAt string
}

type GoldOracle struct {
	httpClient     *http.Client
	sinaPrevClose  float64
	sinaPrevFetched bool
}

func NewGoldOracle() *GoldOracle {
	return &GoldOracle{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (o *GoldOracle) FetchQuote() (*Quote, error) {
	if quote, err := o.fetchGoldAPI(); err == nil && quote.PriceUSD > 0 {
		return quote, nil
	}
	if quote, err := o.fetchGoldSina(); err == nil && quote.PriceUSD > 0 {
		return quote, nil
	}
	return nil, fmt.Errorf("failed to fetch gold price from all sources")
}

func (o *GoldOracle) fetchGoldAPI() (*Quote, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.gold-api.com/price/XAU", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gold-api HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Price             float64 `json:"price"`
		UpdatedAt         string  `json:"updatedAt"`
		UpdatedAtReadable string  `json:"updatedAtReadable"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Price <= 0 {
		return nil, fmt.Errorf("invalid gold-api price")
	}
	prevClose := o.getSinaPrevClose()
	change := 0.0
	if prevClose > 0 {
		change = (payload.Price - prevClose) / prevClose * 100
	}
	updatedAt := payload.UpdatedAtReadable
	if updatedAt == "" {
		updatedAt = payload.UpdatedAt
	}
	return &Quote{
		PriceUSD:       payload.Price,
		Change24h:      change,
		QuoteSource:    "gold-api.com",
		QuoteUpdatedAt: updatedAt,
	}, nil
}

func (o *GoldOracle) getSinaPrevClose() float64 {
	if o.sinaPrevFetched {
		return o.sinaPrevClose
	}
	o.sinaPrevFetched = true
	req, err := http.NewRequest(http.MethodGet, "https://hq.sinajs.cn/list=hf_XAU", nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "PredictionMarket/1.0")
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}
	fields := parseSinaFields(string(body))
	if len(fields) > 1 {
		if v, err := strconv.ParseFloat(fields[1], 64); err == nil {
			o.sinaPrevClose = v
		}
	}
	return o.sinaPrevClose
}

func (o *GoldOracle) fetchGoldSina() (*Quote, error) {
	req, err := http.NewRequest(http.MethodGet, "https://hq.sinajs.cn/list=hf_XAU", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "PredictionMarket/1.0")
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	fields := parseSinaFields(string(body))
	if len(fields) < 2 {
		return nil, fmt.Errorf("invalid sina response")
	}
	price, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || price <= 0 {
		return nil, fmt.Errorf("invalid sina price")
	}
	prevClose, _ := strconv.ParseFloat(fields[1], 64)
	change := 0.0
	if prevClose > 0 {
		change = (price - prevClose) / prevClose * 100
	}
	updatedAt := ""
	if len(fields) > 12 {
		updatedAt = fields[12] + " " + fields[6]
	} else if len(fields) > 6 {
		updatedAt = fields[6]
	}
	return &Quote{
		PriceUSD:       price,
		Change24h:      change,
		QuoteSource:    "新浪财经",
		QuoteUpdatedAt: updatedAt,
	}, nil
}

func parseSinaFields(body string) []string {
	start := strings.Index(body, `"`)
	end := strings.LastIndex(body, `"`)
	if start < 0 || end <= start {
		return nil
	}
	return strings.Split(body[start+1:end], ",")
}
