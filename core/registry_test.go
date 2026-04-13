package core

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func testRegistryClient(t *testing.T, ts *httptest.Server) *RegistryClient {
	t.Helper()
	tsURL, _ := url.Parse(ts.URL)
	return &RegistryClient{
		baseURL:       ts.URL,
		allowedHost:   tsURL.Host,
		allowedScheme: tsURL.Scheme,
		client:        ts.Client(),
		cacheDir:      t.TempDir(),
	}
}

func TestNewRegistryClient_RequiresHTTPS(t *testing.T) {
	_, err := NewRegistryClient("http://insecure.example.com")
	if err == nil {
		t.Error("expected error for non-HTTPS URL")
	}
}

func TestRegistryClient_DownloadSSRFDefense(t *testing.T) {
	// Start an HTTPS test server (test infra uses HTTP, but we test the logic).
	// We bypass the HTTPS check by constructing the client directly.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`Agent.respond("hello")`))
	}))
	defer ts.Close()

	client := testRegistryClient(t, ts)

	// Download from allowed URL should work.
	entry := RegistryEntry{
		ID:  "test-pkg",
		URL: ts.URL + "/packages/test-pkg/main.js",
	}
	dir, err := client.DownloadPackage(entry)
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Error("expected non-empty temp dir")
	}

	// Download from different host should fail.
	entry.URL = "https://evil.com/steal?data=1"
	_, err = client.DownloadPackage(entry)
	if err == nil {
		t.Error("expected SSRF rejection for external URL")
	}
}

func TestRegistryClient_DownloadPathTraversal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("data"))
	}))
	defer ts.Close()

	client := testRegistryClient(t, ts)

	entry := RegistryEntry{
		ID:  "test-pkg",
		URL: ts.URL + "/../../etc/passwd",
	}
	_, err := client.DownloadPackage(entry)
	if err == nil {
		t.Error("expected rejection for path traversal in URL")
	}
}

func TestRegistryClient_DownloadInvalidID(t *testing.T) {
	client := &RegistryClient{
		baseURL:       "https://example.com",
		allowedHost:   "example.com",
		allowedScheme: "https",
		cacheDir:      t.TempDir(),
	}

	entry := RegistryEntry{
		ID:  "../escape",
		URL: "https://example.com/pkg.js",
	}
	_, err := client.DownloadPackage(entry)
	if err == nil {
		t.Error("expected rejection for invalid package ID")
	}
}

func TestRegistryClient_FetchIndex(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			w.Write([]byte(`[{"id":"hello","name":"Hello","version":"1.0.0","url":"https://example.com/hello.js"}]`))
		}
	}))
	defer ts.Close()

	client := testRegistryClient(t, ts)

	entries, err := client.FetchIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if entries[0].ID != "hello" {
		t.Errorf("entries[0].ID = %q", entries[0].ID)
	}
}

func TestRegistryClient_FetchIndexCacheFallback(t *testing.T) {
	// Server that returns 500.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	cacheDir := t.TempDir()

	// Pre-seed cache.
	cacheContent := `[{"id":"cached","name":"Cached Pkg","version":"0.1.0"}]`
	writeFile(t, cacheDir, "index.json", cacheContent)

	tsURL, _ := url.Parse(ts.URL)
	client := &RegistryClient{
		baseURL:       ts.URL,
		allowedHost:   tsURL.Host,
		allowedScheme: tsURL.Scheme,
		client:        ts.Client(),
		cacheDir:      cacheDir,
	}

	entries, err := client.FetchIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "cached" {
		t.Error("should fall back to cached index")
	}
}

func TestRegistryClient_NoRedirect(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("redirected"))
	}))
	defer redirectTarget.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	client := &RegistryClient{
		baseURL:       ts.URL,
		allowedHost:   tsURL.Host,
		allowedScheme: tsURL.Scheme,
		client: &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cacheDir: t.TempDir(),
	}

	entry := RegistryEntry{
		ID:  "redirect-pkg",
		URL: ts.URL + "/redirect",
	}
	_, err := client.DownloadPackage(entry)
	// Should fail because redirect response has non-200 status.
	if err == nil {
		t.Error("expected failure — redirects should not be followed")
	}
}

// writeFile is a test helper that writes a file in a directory.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
