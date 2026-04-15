package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jinto/kittypaw/core"
)

// searchHTTPClient is a shared client for all search backends with a sensible timeout.
var searchHTTPClient = &http.Client{Timeout: 30 * time.Second}

// WebSearchResult holds a single search result.
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchBackend abstracts a web search provider.
type SearchBackend interface {
	Search(ctx context.Context, query string, limit int) ([]WebSearchResult, error)
}

// NewSearchBackend creates a SearchBackend from config.
// When search_backend is empty, auto-detects by key availability: firecrawl > tavily > duckduckgo.
func NewSearchBackend(cfg *core.WebConfig) (SearchBackend, error) {
	backend := strings.ToLower(cfg.SearchBackend)

	// Auto-detect: pick the best available backend.
	if backend == "" {
		switch {
		case cfg.FirecrawlKey != "":
			backend = "firecrawl"
		case cfg.TavilyAPIKey != "":
			backend = "tavily"
		default:
			backend = "duckduckgo"
		}
	}

	switch backend {
	case "firecrawl":
		if cfg.FirecrawlKey == "" {
			return nil, fmt.Errorf("firecrawl search backend requires firecrawl_api_key in [web] config")
		}
		apiURL := cfg.FirecrawlURL
		if apiURL == "" {
			apiURL = "https://api.firecrawl.dev"
		}
		parsed, err := url.Parse(apiURL)
		if err != nil {
			return nil, fmt.Errorf("invalid firecrawl_api_url: %w", err)
		}
		if parsed.Scheme != "https" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
			return nil, fmt.Errorf("firecrawl_api_url must use HTTPS (got %s)", parsed.Scheme)
		}
		return &FirecrawlBackend{APIKey: cfg.FirecrawlKey, BaseURL: strings.TrimRight(apiURL, "/")}, nil
	case "tavily":
		if cfg.TavilyAPIKey == "" {
			return nil, fmt.Errorf("tavily search backend requires tavily_api_key in [web] config")
		}
		return &TavilyBackend{APIKey: cfg.TavilyAPIKey}, nil
	case "duckduckgo":
		return &DuckDuckGoBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown search backend: %q (supported: firecrawl, tavily, duckduckgo)", backend)
	}
}

// --- DuckDuckGo ---

// DuckDuckGoBackend searches via DuckDuckGo HTML (free, no API key).
type DuckDuckGoBackend struct{}

func (d *DuckDuckGoBackend) Search(ctx context.Context, query string, limit int) ([]WebSearchResult, error) {
	u := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "KittyPaw/1.0")

	resp, err := searchHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	return extractWebSearchResults(string(body), limit), nil
}

// extractWebSearchResults parses DuckDuckGo HTML results.
// Moved from executor.go — regex-free string splitting.
func extractWebSearchResults(htmlBody string, limit int) []WebSearchResult {
	if limit <= 0 {
		limit = 10
	}
	var results []WebSearchResult
	parts := strings.Split(htmlBody, "result__a")
	for i, part := range parts {
		if i == 0 {
			continue
		}
		// Extract href
		hrefIdx := strings.Index(part, "href=\"")
		if hrefIdx == -1 {
			continue
		}
		href := part[hrefIdx+6:]
		hrefEnd := strings.Index(href, "\"")
		if hrefEnd == -1 {
			continue
		}
		rawURL := href[:hrefEnd]

		// Extract title text (between > and </a>)
		titleStart := strings.Index(part, ">")
		if titleStart == -1 {
			continue
		}
		titleEnd := strings.Index(part[titleStart:], "</a>")
		if titleEnd == -1 {
			continue
		}
		title := stripHTMLTags(part[titleStart+1 : titleStart+titleEnd])

		// Extract snippet
		snippet := ""
		snippetIdx := strings.Index(part, "result__snippet")
		if snippetIdx != -1 {
			snipStart := strings.Index(part[snippetIdx:], ">")
			if snipStart != -1 {
				snipEnd := strings.Index(part[snippetIdx+snipStart:], "</")
				if snipEnd != -1 {
					snippet = stripHTMLTags(part[snippetIdx+snipStart+1 : snippetIdx+snipStart+snipEnd])
				}
			}
		}

		results = append(results, WebSearchResult{
			Title:   html.UnescapeString(strings.TrimSpace(title)),
			URL:     cleanDuckDuckGoURL(rawURL),
			Snippet: html.UnescapeString(strings.TrimSpace(snippet)),
		})
		if len(results) >= limit {
			break
		}
	}
	return results
}

// cleanDuckDuckGoURL extracts the real URL from a DuckDuckGo redirect link.
func cleanDuckDuckGoURL(raw string) string {
	raw = html.UnescapeString(raw)
	if strings.Contains(raw, "duckduckgo.com/l/") {
		if u, err := url.Parse(raw); err == nil {
			if real := u.Query().Get("uddg"); real != "" {
				return real
			}
		}
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

// --- Tavily ---

// TavilyBackend searches via the Tavily REST API (requires API key).
type TavilyBackend struct {
	APIKey string
}

func (t *TavilyBackend) Search(ctx context.Context, query string, limit int) ([]WebSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query":       query,
		"max_results": limit,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	resp, err := searchHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1000))
		return nil, fmt.Errorf("tavily API error %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	if err != nil {
		return nil, err
	}

	var tavilyResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &tavilyResp); err != nil {
		return nil, fmt.Errorf("tavily response parse: %w", err)
	}

	results := make([]WebSearchResult, 0, len(tavilyResp.Results))
	for _, r := range tavilyResp.Results {
		results = append(results, WebSearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}
	return results, nil
}

// --- Firecrawl ---

// FirecrawlBackend searches via the Firecrawl REST API.
type FirecrawlBackend struct {
	APIKey  string
	BaseURL string // e.g. "https://api.firecrawl.dev"
}

func (f *FirecrawlBackend) Search(ctx context.Context, query string, limit int) ([]WebSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	reqBody, _ := json.Marshal(map[string]any{
		"query": query,
		"limit": limit,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", f.BaseURL+"/v1/search", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.APIKey)

	resp, err := searchHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1000))
		return nil, fmt.Errorf("firecrawl API error %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	if err != nil {
		return nil, err
	}

	var fcResp struct {
		Success bool `json:"success"`
		Data    []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Markdown    string `json:"markdown"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &fcResp); err != nil {
		return nil, fmt.Errorf("firecrawl response parse: %w", err)
	}

	results := make([]WebSearchResult, 0, len(fcResp.Data))
	for _, d := range fcResp.Data {
		snippet := d.Description
		if snippet == "" {
			snippet = truncate(d.Markdown, 300)
		}
		results = append(results, WebSearchResult{
			Title:   d.Title,
			URL:     d.URL,
			Snippet: snippet,
		})
	}
	return results, nil
}
