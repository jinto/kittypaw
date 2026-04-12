package engine

import (
	"testing"
)

func TestIsPathAllowed(t *testing.T) {
	tests := []struct {
		path    string
		allowed []string
		want    bool
	}{
		// No allowed paths → deny all
		{"/tmp/file.txt", nil, false},
		{"/tmp/file.txt", []string{}, false},

		// Exact match
		{"/tmp/safe", []string{"/tmp/safe"}, true},

		// Subdirectory
		{"/tmp/safe/file.txt", []string{"/tmp/safe"}, true},
		{"/tmp/safe/sub/deep", []string{"/tmp/safe"}, true},

		// Separator boundary — the critical security fix
		{"/tmp/safe-evil/file.txt", []string{"/tmp/safe"}, false},
		{"/tmp/safefile", []string{"/tmp/safe"}, false},

		// Multiple allowed paths
		{"/home/user/file", []string{"/tmp", "/home/user"}, true},
		{"/etc/passwd", []string{"/tmp", "/home/user"}, false},
	}
	for _, tt := range tests {
		got := isPathAllowed(tt.path, tt.allowed)
		if got != tt.want {
			t.Errorf("isPathAllowed(%q, %v) = %v, want %v", tt.path, tt.allowed, got, tt.want)
		}
	}
}

func TestValidateHTTPTarget(t *testing.T) {
	tests := []struct {
		url     string
		allowed []string
		wantErr bool
	}{
		// Public URL, no restrictions
		{"https://example.com/api", nil, false},
		{"https://example.com/api", []string{}, false},

		// Private IPs blocked
		{"http://127.0.0.1:8080/admin", nil, true},
		{"http://localhost/secret", nil, true},
		{"http://10.0.0.1/internal", nil, true},
		{"http://192.168.1.1/router", nil, true},
		{"http://169.254.1.1/metadata", nil, true},

		// AllowedHosts whitelist
		{"https://api.example.com/data", []string{"api.example.com"}, false},
		{"https://evil.com/data", []string{"api.example.com"}, true},

		// Wildcard in allowed hosts
		{"https://anything.com/path", []string{"*"}, false},

		// Invalid URL
		{"://bad", nil, true},
	}
	for _, tt := range tests {
		err := validateHTTPTarget(tt.url, tt.allowed)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateHTTPTarget(%q, %v) error = %v, wantErr %v", tt.url, tt.allowed, err, tt.wantErr)
		}
	}
}

func TestStripHTMLTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<p>hello</p>", "hello"},
		{"no tags", "no tags"},
		{"<b>bold</b> and <i>italic</i>", "bold and italic"},
		{"<a href=\"url\">link</a>", "link"},
		{"", ""},
		{"<>empty tag</>", "empty tag"},
		{"nested <div><span>text</span></div>", "nested text"},
	}
	for _, tt := range tests {
		got := stripHTMLTags(tt.input)
		if got != tt.want {
			t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractSearchResults(t *testing.T) {
	// Minimal DuckDuckGo-like HTML with result__a class
	html := `<div class="result__a" href="https://example.com">
>Example Title</a>
<div class="result__snippet">A test snippet</div>
</div>
<div class="result__a" href="https://other.com">
>Other Title</a>
</div>`

	results := extractSearchResults(html)
	if len(results) == 0 {
		t.Fatal("extractSearchResults returned no results")
	}
	if results[0]["url"] != "https://example.com" {
		t.Errorf("result[0].url = %q, want %q", results[0]["url"], "https://example.com")
	}
}

func TestExtractSearchResultsEmpty(t *testing.T) {
	results := extractSearchResults("<html><body>no results</body></html>")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestExtractSearchResultsMaxTen(t *testing.T) {
	// Build HTML with 15 results
	html := ""
	for i := 0; i < 15; i++ {
		html += `<div class="result__a" href="https://example.com">` +
			`>Title</a></div>`
	}
	results := extractSearchResults(html)
	if len(results) > 10 {
		t.Errorf("expected at most 10 results, got %d", len(results))
	}
}
