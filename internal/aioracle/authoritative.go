package aioracle

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

var authoritativeHosts = []string{
	"federalreserve.gov",
	"bls.gov",
	"bea.gov",
	"cmegroup.com",
	"gold-api.com",
}

func fetchAuthoritativeEvidence(ctx context.Context, sources []string) ([]NewsArticle, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var out []NewsArticle
	for _, raw := range sources {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme != "https" || !allowedAuthoritativeHost(parsed.Hostname()) {
			return nil, fmt.Errorf("authoritative source is not allowlisted: %s", raw)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "PredictionMarket-AIOracle/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("authoritative source %s returned HTTP %d", parsed.Hostname(), resp.StatusCode)
		}
		text := strings.Join(strings.Fields(html.UnescapeString(htmlTagPattern.ReplaceAllString(string(body), " "))), " ")
		if len(text) > 5000 {
			text = text[:5000]
		}
		out = append(out, NewsArticle{Title: "Official evidence from " + parsed.Hostname(), URL: parsed.String(), Source: parsed.Hostname(), PublishedAt: time.Now(), Content: text})
	}
	return out, nil
}

func allowedAuthoritativeHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, allowed := range authoritativeHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}
