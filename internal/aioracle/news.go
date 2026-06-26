package aioracle

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewsFetcher retrieves relevant news articles for an event.
type NewsFetcher interface {
	// Fetch retrieves news articles matching the given keywords published
	// after `since`. Returns at most maxArticles results.
	Fetch(ctx context.Context, keywords []string, since time.Time, maxArticles int) ([]NewsArticle, error)
}

// =============================================================================
// Composite fetcher: tries multiple sources and merges results
// =============================================================================

// CompositeNewsFetcher tries each sub-fetcher in order until enough articles
// are collected, then de-duplicates and returns the combined set.
type CompositeNewsFetcher struct {
	fetchers []NewsFetcher
}

// NewCompositeNewsFetcher creates a fetcher that aggregates multiple sources.
func NewCompositeNewsFetcher(fetchers ...NewsFetcher) *CompositeNewsFetcher {
	return &CompositeNewsFetcher{fetchers: fetchers}
}

func (c *CompositeNewsFetcher) Fetch(ctx context.Context, keywords []string, since time.Time, maxArticles int) ([]NewsArticle, error) {
	if maxArticles <= 0 {
		maxArticles = 10
	}

	var all []NewsArticle
	seen := make(map[string]bool)

	for _, f := range c.fetchers {
		if len(all) >= maxArticles {
			break
		}
		articles, err := f.Fetch(ctx, keywords, since, maxArticles-len(all))
		if err != nil {
			// Log but continue with other fetchers — one broken source
			// shouldn't prevent the oracle from working.
			continue
		}
		for _, a := range articles {
			key := strings.ToLower(strings.TrimSpace(a.URL))
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(a.Title))
			}
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, a)
			if len(all) >= maxArticles {
				break
			}
		}
	}
	return all, nil
}

// =============================================================================
// RSS feed fetcher
// =============================================================================

// RSSFetcher pulls articles from RSS/Atom feeds.
type RSSFetcher struct {
	feeds      []string
	httpClient *http.Client
}

// NewRSSFetcher creates a fetcher that reads the given RSS feed URLs.
func NewRSSFetcher(feeds []string, timeout time.Duration) *RSSFetcher {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &RSSFetcher{
		feeds:      feeds,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// rssDocument is a minimal RSS 2.0 parser.
type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string    `xml:"title"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

// atomFeed is a minimal Atom parser.
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string   `xml:"title"`
	Link    atomLink `xml:"link"`
	Summary string   `xml:"summary"`
	Updated string   `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

func (f *RSSFetcher) Fetch(ctx context.Context, keywords []string, since time.Time, maxArticles int) ([]NewsArticle, error) {
	if maxArticles <= 0 {
		maxArticles = 10
	}

	var articles []NewsArticle
	for _, feedURL := range f.feeds {
		if len(articles) >= maxArticles {
			break
		}
		feedArticles, err := f.fetchFeed(ctx, feedURL, since)
		if err != nil {
			continue
		}
		for _, a := range feedArticles {
			if !matchesKeywords(a, keywords) {
				continue
			}
			articles = append(articles, a)
			if len(articles) >= maxArticles {
				break
			}
		}
	}
	return articles, nil
}

func (f *RSSFetcher) fetchFeed(ctx context.Context, feedURL string, since time.Time) ([]NewsArticle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PredictionMarket-AIOracle/1.0")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("RSS feed HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Try RSS 2.0 first.
	var rss rssDocument
	if err := xml.Unmarshal(raw, &rss); err == nil && len(rss.Channel.Items) > 0 {
		var out []NewsArticle
		for _, item := range rss.Channel.Items {
			pubTime := parseRSSDate(item.PubDate)
			if pubTime.Before(since) {
				continue
			}
			out = append(out, NewsArticle{
				Title:       item.Title,
				URL:         item.Link,
				Source:      rss.Channel.Title,
				PublishedAt: pubTime,
				Content:     item.Description,
			})
		}
		return out, nil
	}

	// Try Atom.
	var atom atomFeed
	if err := xml.Unmarshal(raw, &atom); err == nil && len(atom.Entries) > 0 {
		var out []NewsArticle
		for _, entry := range atom.Entries {
			pubTime := parseRSSDate(entry.Updated)
			if pubTime.Before(since) {
				continue
			}
			out = append(out, NewsArticle{
				Title:       entry.Title,
				URL:         entry.Link.Href,
				Source:      atom.Title,
				PublishedAt: pubTime,
				Content:     entry.Summary,
			})
		}
		return out, nil
	}

	return nil, fmt.Errorf("unrecognized feed format for %s", feedURL)
}

// =============================================================================
// NewsAPI.org fetcher
// =============================================================================

// NewsAPIFetcher uses newsapi.org to search for articles.
type NewsAPIFetcher struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewNewsAPIFetcher creates a fetcher backed by newsapi.org.
func NewNewsAPIFetcher(apiKey, baseURL string, timeout time.Duration) *NewsAPIFetcher {
	if baseURL == "" {
		baseURL = "https://newsapi.org/v2/everything"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &NewsAPIFetcher{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type newsAPIResponse struct {
	Status   string `json:"status"`
	Articles []struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Source      struct{ Name string } `json:"source"`
		PublishedAt string `json:"publishedAt"`
		Description string `json:"description"`
		Content     string `json:"content"`
	} `json:"articles"`
}

func (f *NewsAPIFetcher) Fetch(ctx context.Context, keywords []string, since time.Time, maxArticles int) ([]NewsArticle, error) {
	if f.apiKey == "" {
		return nil, fmt.Errorf("newsapi: api_key not configured")
	}
	if maxArticles <= 0 {
		maxArticles = 10
	}

	query := strings.Join(keywords, " OR ")
	params := url.Values{}
	params.Set("q", query)
	params.Set("from", since.UTC().Format("2006-01-02T15:04:05"))
	params.Set("sortBy", "relevancy")
	params.Set("pageSize", fmt.Sprintf("%d", maxArticles))
	params.Set("language", "en")

	reqURL := f.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PredictionMarket-AIOracle/1.0")
	req.Header.Set("X-Api-Key", f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("newsapi HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result newsAPIResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result.Status != "ok" {
		return nil, fmt.Errorf("newsapi status: %s", result.Status)
	}

	var articles []NewsArticle
	for _, a := range result.Articles {
		pubTime, _ := time.Parse(time.RFC3339, a.PublishedAt)
		if pubTime.Before(since) {
			continue
		}
		content := a.Description
		if content == "" {
			content = a.Content
		}
		articles = append(articles, NewsArticle{
			Title:       a.Title,
			URL:         a.URL,
			Source:      a.Source.Name,
			PublishedAt: pubTime,
			Content:     content,
		})
		if len(articles) >= maxArticles {
			break
		}
	}
	return articles, nil
}

// =============================================================================
// Helpers
// =============================================================================

var rssDateFormats = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC3339,
	"Mon, 02 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04:05 -0700",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02T15:04:05Z",
	"2006-01-02",
}

func parseRSSDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range rssDateFormats {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func matchesKeywords(article NewsArticle, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	text := strings.ToLower(article.Title + " " + article.Content)
	for _, kw := range keywords {
		if strings.Contains(text, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
