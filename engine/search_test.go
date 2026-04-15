package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestNewSearchBackend_Default(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := b.(*DuckDuckGoBackend); !ok {
		t.Errorf("expected DuckDuckGoBackend, got %T", b)
	}
}

func TestNewSearchBackend_DuckDuckGo(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{SearchBackend: "duckduckgo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := b.(*DuckDuckGoBackend); !ok {
		t.Errorf("expected DuckDuckGoBackend, got %T", b)
	}
}

func TestNewSearchBackend_Tavily(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{SearchBackend: "tavily", TavilyAPIKey: "test-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tb, ok := b.(*TavilyBackend)
	if !ok {
		t.Fatalf("expected TavilyBackend, got %T", b)
	}
	if tb.APIKey != "test-key" {
		t.Errorf("expected API key 'test-key', got %q", tb.APIKey)
	}
}

func TestNewSearchBackend_TavilyNoKey(t *testing.T) {
	_, err := NewSearchBackend(&core.WebConfig{SearchBackend: "tavily"})
	if err == nil {
		t.Fatal("expected error for tavily without API key")
	}
}

func TestNewSearchBackend_Firecrawl(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{SearchBackend: "firecrawl", FirecrawlKey: "fc-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fb, ok := b.(*FirecrawlBackend)
	if !ok {
		t.Fatalf("expected FirecrawlBackend, got %T", b)
	}
	if fb.APIKey != "fc-key" {
		t.Errorf("expected API key 'fc-key', got %q", fb.APIKey)
	}
	if fb.BaseURL != "https://api.firecrawl.dev" {
		t.Errorf("expected default base URL, got %q", fb.BaseURL)
	}
}

func TestNewSearchBackend_FirecrawlSelfHosted(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{
		SearchBackend: "firecrawl",
		FirecrawlKey:  "fc-key",
		FirecrawlURL:  "https://my-firecrawl.example.com/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fb := b.(*FirecrawlBackend)
	if fb.BaseURL != "https://my-firecrawl.example.com" {
		t.Errorf("expected trailing slash stripped, got %q", fb.BaseURL)
	}
}

func TestNewSearchBackend_FirecrawlNoKey(t *testing.T) {
	_, err := NewSearchBackend(&core.WebConfig{SearchBackend: "firecrawl"})
	if err == nil {
		t.Fatal("expected error for firecrawl without API key")
	}
}

func TestNewSearchBackend_FirecrawlRejectsHTTP(t *testing.T) {
	_, err := NewSearchBackend(&core.WebConfig{
		SearchBackend: "firecrawl",
		FirecrawlKey:  "fc-key",
		FirecrawlURL:  "http://evil.example.com",
	})
	if err == nil {
		t.Fatal("expected error for non-HTTPS firecrawl URL")
	}
}

func TestNewSearchBackend_FirecrawlAllowsLocalhost(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{
		SearchBackend: "firecrawl",
		FirecrawlKey:  "fc-key",
		FirecrawlURL:  "http://localhost:3002",
	})
	if err != nil {
		t.Fatalf("localhost should be allowed over HTTP: %v", err)
	}
	if _, ok := b.(*FirecrawlBackend); !ok {
		t.Errorf("expected FirecrawlBackend, got %T", b)
	}
}

func TestNewSearchBackend_AutoDetectFirecrawl(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{
		FirecrawlKey: "fc-key",
		TavilyAPIKey: "tv-key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := b.(*FirecrawlBackend); !ok {
		t.Errorf("auto-detect should prefer FirecrawlBackend, got %T", b)
	}
}

func TestNewSearchBackend_AutoDetectTavily(t *testing.T) {
	b, err := NewSearchBackend(&core.WebConfig{TavilyAPIKey: "tv-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := b.(*TavilyBackend); !ok {
		t.Errorf("auto-detect should pick TavilyBackend when no firecrawl key, got %T", b)
	}
}

func TestNewSearchBackend_Unknown(t *testing.T) {
	_, err := NewSearchBackend(&core.WebConfig{SearchBackend: "google"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestDDGExtractSearchResults(t *testing.T) {
	// Minimal DuckDuckGo-like HTML structure
	html := `<div class="results">
		<div class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Farticle&rut=abc">
			<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Farticle&amp;rut=abc">Example Article</a>
			<div class="result__snippet">This is a snippet about the article.</div>
		</div>
	</div>`

	results := extractWebSearchResults(html, 10)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	r := results[0]
	if r.Title != "Example Article" {
		t.Errorf("title = %q, want 'Example Article'", r.Title)
	}
	if r.Snippet != "This is a snippet about the article." {
		t.Errorf("snippet = %q", r.Snippet)
	}
}

func TestDDGExtractSearchResults_Limit(t *testing.T) {
	// Build HTML with 5 results
	var html string
	for i := 0; i < 5; i++ {
		html += `<a class="result__a" href="https://example.com">Title</a>`
	}
	results := extractWebSearchResults(html, 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestCleanDuckDuckGoURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.bbc.com%2Farticle&rut=x", "https://www.bbc.com/article"},
		{"//example.com/page", "https://example.com/page"},
		{"https://direct.com/path", "https://direct.com/path"},
	}
	for _, tc := range tests {
		got := cleanDuckDuckGoURL(tc.input)
		if got != tc.want {
			t.Errorf("cleanDuckDuckGoURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFirecrawlBackend_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/search" {
			t.Errorf("expected /v1/search, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-fc-key" {
			t.Errorf("expected Bearer auth, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"url": "https://example.com/article1",
					"title": "Breaking News: AI Advances",
					"description": "New AI models show remarkable improvements in reasoning."
				},
				{
					"url": "https://example.com/article2",
					"title": "Economy Update",
					"description": ""
				}
			]
		}`))
	}))
	defer srv.Close()

	fb := &FirecrawlBackend{APIKey: "test-fc-key", BaseURL: srv.URL}
	results, err := fb.Search(context.Background(), "AI news today", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Breaking News: AI Advances" {
		t.Errorf("title = %q", results[0].Title)
	}
	if results[0].Snippet != "New AI models show remarkable improvements in reasoning." {
		t.Errorf("snippet = %q", results[0].Snippet)
	}
	if results[0].URL != "https://example.com/article1" {
		t.Errorf("url = %q", results[0].URL)
	}
}

func TestFirecrawlBackend_MarkdownFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"url": "https://example.com/page",
					"title": "Page Title",
					"description": "",
					"markdown": "This is the full page content in markdown format with lots of detail."
				}
			]
		}`))
	}))
	defer srv.Close()

	fb := &FirecrawlBackend{APIKey: "key", BaseURL: srv.URL}
	results, err := fb.Search(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// When description is empty, snippet should fall back to truncated markdown.
	if results[0].Snippet == "" {
		t.Error("expected snippet to fall back to markdown content")
	}
}

func TestFirecrawlBackend_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer srv.Close()

	fb := &FirecrawlBackend{APIKey: "bad-key", BaseURL: srv.URL}
	_, err := fb.Search(context.Background(), "test", 5)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}
