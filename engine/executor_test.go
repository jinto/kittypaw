package engine

import (
	"os"
	"path/filepath"
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

func TestIsPathAllowedSymlinkParent(t *testing.T) {
	// Create a real directory structure with symlinks to test parent resolution.
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside the allowed dir that points outside.
	symlinkPath := filepath.Join(allowedDir, "escape")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	allowed := []string{allowedDir}

	// Existing file via symlink — should be denied (resolves to outside).
	existingFile := filepath.Join(outsideDir, "secret.txt")
	os.WriteFile(existingFile, []byte("secret"), 0o644)
	if isPathAllowed(filepath.Join(allowedDir, "escape", "secret.txt"), allowed) {
		t.Error("existing file via symlink to outside should be denied")
	}

	// Non-existent file via symlink — the critical bug fix.
	// Without parent walk, this would be allowed because EvalSymlinks fails on
	// non-existent files, leaving the unresolved path that starts with allowedDir.
	if isPathAllowed(filepath.Join(allowedDir, "escape", "newfile.txt"), allowed) {
		t.Error("non-existent file via parent symlink to outside should be denied")
	}

	// Legitimate file within allowed dir should still work.
	if !isPathAllowed(filepath.Join(allowedDir, "safe.txt"), allowed) {
		t.Error("file directly in allowed dir should be allowed")
	}

	// Non-existent file within allowed dir (no symlinks) should be allowed.
	if !isPathAllowed(filepath.Join(allowedDir, "newfile.txt"), allowed) {
		t.Error("non-existent file in allowed dir should be allowed")
	}

	// Deep nested non-existent file in allowed dir.
	if !isPathAllowed(filepath.Join(allowedDir, "sub", "deep", "file.txt"), allowed) {
		t.Error("deep non-existent file in allowed dir should be allowed")
	}
}

func TestResolveForValidation(t *testing.T) {
	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	os.MkdirAll(realDir, 0o755)

	// Resolve the real dir itself (macOS: /var → /private/var).
	resolvedRealDir, _ := filepath.EvalSymlinks(realDir)

	// Symlink: tmpDir/link → tmpDir/real
	linkPath := filepath.Join(tmpDir, "link")
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Existing file through symlink.
	os.WriteFile(filepath.Join(realDir, "exists.txt"), []byte("hi"), 0o644)
	resolved := resolveForValidation(filepath.Join(linkPath, "exists.txt"))
	expected := filepath.Join(resolvedRealDir, "exists.txt")
	if resolved != expected {
		t.Errorf("existing file: got %q, want %q", resolved, expected)
	}

	// Non-existent file through symlink — should still resolve parent.
	resolved = resolveForValidation(filepath.Join(linkPath, "nofile.txt"))
	expected = filepath.Join(resolvedRealDir, "nofile.txt")
	if resolved != expected {
		t.Errorf("non-existent file: got %q, want %q", resolved, expected)
	}

	// Non-existent deep path through symlink.
	resolved = resolveForValidation(filepath.Join(linkPath, "a", "b", "c.txt"))
	expected = filepath.Join(resolvedRealDir, "a", "b", "c.txt")
	if resolved != expected {
		t.Errorf("deep non-existent: got %q, want %q", resolved, expected)
	}
}

func TestFileSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()
	allowed := []string{tmpDir}

	// Create a file just over the limit.
	bigFile := filepath.Join(tmpDir, "big.bin")
	f, err := os.Create(bigFile)
	if err != nil {
		t.Fatal(err)
	}
	// Write 10MB + 1 byte.
	if err := f.Truncate(maxFileReadSize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	// Verify the constant is 10MB.
	if maxFileReadSize != 10*1024*1024 {
		t.Fatalf("maxFileReadSize = %d, want 10MB", maxFileReadSize)
	}

	// File within limit should work (we just check isPathAllowed + size gate here).
	smallFile := filepath.Join(tmpDir, "small.txt")
	os.WriteFile(smallFile, []byte("hello"), 0o644)

	// Verify small file is allowed.
	if !isPathAllowed(smallFile, allowed) {
		t.Error("small file should be in allowed path")
	}

	// Verify big file is allowed path-wise (the size limit is in executeFile, not isPathAllowed).
	if !isPathAllowed(bigFile, allowed) {
		t.Error("big file should be in allowed path")
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
