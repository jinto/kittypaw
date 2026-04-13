package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RegistryEntry describes a package listing in the remote registry.
type RegistryEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	URL         string `json:"url"`
}

// RegistryClient fetches package listings and downloads from a remote registry.
type RegistryClient struct {
	baseURL       string
	allowedHost   string // exact host match for SSRF defense
	allowedScheme string // "https" enforced at construction
	client        *http.Client
	cacheDir      string
}

// NewRegistryClient creates a client pointing at the given registry URL.
// Downloads are only allowed from URLs matching the base URL prefix (SSRF defense).
func NewRegistryClient(baseURL string) (*RegistryClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("registry URL must use HTTPS: %s", baseURL)
	}

	cacheDir, err := registryCacheDir()
	if err != nil {
		return nil, err
	}

	return &RegistryClient{
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		allowedHost:   parsed.Host,
		allowedScheme: parsed.Scheme,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse // no redirects
			},
		},
		cacheDir: cacheDir,
	}, nil
}

// FetchIndex retrieves the package listing from the registry.
// Falls back to a cached copy on network failure.
func (rc *RegistryClient) FetchIndex() ([]RegistryEntry, error) {
	resp, err := rc.client.Get(rc.baseURL + "/index.json")
	if err != nil {
		return rc.loadCachedIndex()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return rc.loadCachedIndex()
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB max
	if err != nil {
		return rc.loadCachedIndex()
	}

	// Cache the response for offline use.
	cachePath := filepath.Join(rc.cacheDir, "index.json")
	_ = os.WriteFile(cachePath, body, 0o644)

	var entries []RegistryEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parse registry index: %w", err)
	}
	return entries, nil
}

// DownloadPackage downloads and extracts a package to a temporary directory.
// Returns the path to the temp directory containing the package files.
// The caller is responsible for removing the directory.
func (rc *RegistryClient) DownloadPackage(entry RegistryEntry) (string, error) {
	if err := ValidatePackageID(entry.ID); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	// SSRF defense: parse URL and compare host exactly (not prefix).
	parsed, err := url.Parse(entry.URL)
	if err != nil {
		return "", fmt.Errorf("download: invalid URL: %w", err)
	}
	if parsed.Host != rc.allowedHost {
		return "", fmt.Errorf("download URL host %q not allowed (must be %s)", parsed.Host, rc.allowedHost)
	}
	if parsed.Scheme != rc.allowedScheme {
		return "", fmt.Errorf("download URL scheme %q not allowed (must be %s)", parsed.Scheme, rc.allowedScheme)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("download URL must not contain userinfo: %s", entry.URL)
	}
	if strings.Contains(parsed.Path, "..") {
		return "", fmt.Errorf("download: URL contains path traversal: %s", entry.URL)
	}

	resp, err := rc.client.Get(entry.URL)
	if err != nil {
		return "", fmt.Errorf("download %q: %w", entry.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %q: HTTP %d", entry.ID, resp.StatusCode)
	}

	// Read the package tarball/content to a temp directory.
	tmpDir, err := os.MkdirTemp("", "gopaw-pkg-"+entry.ID+"-")
	if err != nil {
		return "", fmt.Errorf("download: create temp dir: %w", err)
	}

	// For now, assume the download URL points to a single main.js + package.toml.
	// A real implementation would handle tar.gz archives.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB max
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download %q: read body: %w", entry.ID, err)
	}

	// Write as main.js (simplified — real impl would extract archive).
	if err := os.WriteFile(filepath.Join(tmpDir, "main.js"), body, 0o644); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	return tmpDir, nil
}

// loadCachedIndex reads the locally cached index.
func (rc *RegistryClient) loadCachedIndex() ([]RegistryEntry, error) {
	cachePath := filepath.Join(rc.cacheDir, "index.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("no cached registry index and network unavailable")
	}

	var entries []RegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse cached index: %w", err)
	}
	return entries, nil
}

// registryCacheDir returns the directory for registry cache files.
func registryCacheDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dir, "cache", "registry")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	return cacheDir, nil
}
