package engine

import (
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
