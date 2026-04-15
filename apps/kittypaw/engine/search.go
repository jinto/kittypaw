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

	"github.com/jinto/kittypaw/core"
)

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
// Returns DuckDuckGoBackend when unconfigured or set to "duckduckgo".
func NewSearchBackend(cfg *core.WebConfig) (SearchBackend, error) {
	switch strings.ToLower(cfg.SearchBackend) {
	case "tavily":
		if cfg.TavilyAPIKey == "" {
			return nil, fmt.Errorf("tavily search backend requires tavily_api_key in [web] config")
		}
		return &TavilyBackend{APIKey: cfg.TavilyAPIKey}, nil
	case "duckduckgo", "":
		return &DuckDuckGoBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown search backend: %q (supported: duckduckgo, tavily)", cfg.SearchBackend)
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

	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.DefaultClient.Do(req)
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
