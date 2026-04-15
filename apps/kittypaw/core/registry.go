package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRegistryURL is the canonical GitHub-hosted skill registry.
const DefaultRegistryURL = "https://raw.githubusercontent.com/kittypaw-skills/registry/main"

// RegistryEntry describes a package listing in the remote registry.
type RegistryEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	URL         string `json:"url,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	Hash        string `json:"hash,omitempty"` // SHA256 content hash for verification
}

// EffectiveURL returns the package download URL, preferring download_url over url.
func (e RegistryEntry) EffectiveURL() string {
	if e.DownloadURL != "" {
		return e.DownloadURL
	}
	return e.URL
}

// registryIndex is the envelope format for index.json.
type registryIndex struct {
	Version  int             `json:"version"`
	Packages []RegistryEntry `json:"packages"`
}

// RegistryClient fetches package listings and downloads from a remote registry.
type RegistryClient struct {
	baseURL         string
	allowedHost     string // exact host match for SSRF defense
	allowedScheme   string // "https" enforced at construction
	allowedBasePath string // path prefix from base URL for SSRF defense
	client          *http.Client
	cacheDir        string
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
		baseURL:         strings.TrimSuffix(baseURL, "/"),
		allowedHost:     parsed.Host,
		allowedScheme:   parsed.Scheme,
		allowedBasePath: strings.TrimSuffix(parsed.Path, "/") + "/",
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse // no redirects
			},
		},
		cacheDir: cacheDir,
	}, nil
}

// IndexResult wraps the registry entries with cache metadata.
type IndexResult struct {
	Entries   []RegistryEntry
	FromCache bool
	CachedAt  time.Time // zero if live or cache time unknown
}

// FetchIndex retrieves the package listing from the registry.
// Falls back to a cached copy on network failure.
func (rc *RegistryClient) FetchIndex() ([]RegistryEntry, error) {
	result, err := rc.FetchIndexWithMeta()
	if err != nil {
		return nil, err
	}
	return result.Entries, nil
}

// cachedResult returns the cached index wrapped as an IndexResult.
func (rc *RegistryClient) cachedResult() (*IndexResult, error) {
	entries, err := rc.loadCachedIndex()
	if err != nil {
		return nil, err
	}
	return &IndexResult{Entries: entries, FromCache: true, CachedAt: rc.cacheModTime()}, nil
}

// FetchIndexWithMeta retrieves the package listing with cache metadata.
func (rc *RegistryClient) FetchIndexWithMeta() (*IndexResult, error) {
	resp, err := rc.client.Get(rc.baseURL + "/index.json")
	if err != nil {
		return rc.cachedResult()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return rc.cachedResult()
	}

	const maxIndexSize = 2 << 20 // 2MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexSize+1))
	if err != nil {
		return rc.cachedResult()
	}
	if len(body) > maxIndexSize {
		return nil, fmt.Errorf("registry index too large (>%d bytes)", maxIndexSize)
	}

	entries, err := parseIndexJSON(body)
	if err != nil {
		return nil, err
	}

	// Cache only after successful parse to prevent cache poisoning.
	cachePath := filepath.Join(rc.cacheDir, "index.json")
	_ = os.WriteFile(cachePath, body, 0o600)

	return &IndexResult{Entries: entries, FromCache: false}, nil
}

// parseIndexJSON handles both formats: bare array and {"version":N,"packages":[...]} wrapper.
func parseIndexJSON(data []byte) ([]RegistryEntry, error) {
	// Try wrapped format first.
	var idx registryIndex
	if err := json.Unmarshal(data, &idx); err == nil && len(idx.Packages) > 0 {
		return idx.Packages, nil
	}
	// Fall back to bare array.
	var entries []RegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse registry index: %w", err)
	}
	return entries, nil
}

// cacheModTime returns the modification time of the cached index file.
func (rc *RegistryClient) cacheModTime() time.Time {
	fi, err := os.Stat(filepath.Join(rc.cacheDir, "index.json"))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// DownloadPackage downloads a package's files to a temporary directory.
// entry.URL is treated as a directory URL — individual files are fetched by
// appending hardcoded filenames (package.toml, main.js, README.md).
// Returns the path to the temp directory containing the package files.
// The caller is responsible for removing the directory.
func (rc *RegistryClient) DownloadPackage(entry RegistryEntry) (string, error) {
	if err := ValidatePackageID(entry.ID); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	// SSRF defense: validate the base URL once. File names are hardcoded constants,
	// so no additional validation is needed for the individual file URLs.
	dlURL := entry.EffectiveURL()
	if err := rc.validateURL(dlURL); err != nil {
		return "", err
	}

	baseURL := strings.TrimSuffix(dlURL, "/")

	tmpDir, err := os.MkdirTemp("", "kittypaw-pkg-"+entry.ID+"-")
	if err != nil {
		return "", fmt.Errorf("download: create temp dir: %w", err)
	}

	// Required files — failure removes tmpDir.
	for _, name := range []string{"package.toml", "main.js"} {
		if err := rc.fetchToFile(baseURL+"/"+name, filepath.Join(tmpDir, name)); err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("download %q: %s: %w", entry.ID, name, err)
		}
	}

	// Optional files — only 404 is ignored; other errors propagate.
	if err := rc.fetchToFile(baseURL+"/README.md", filepath.Join(tmpDir, "README.md")); err != nil && !isHTTPNotFound(err) {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("download %q: README.md: %w", entry.ID, err)
	}

	return tmpDir, nil
}

// validateURL performs SSRF checks on a download URL.
func (rc *RegistryClient) validateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("download: invalid URL: %w", err)
	}
	if parsed.Host != rc.allowedHost {
		return fmt.Errorf("download URL host %q not allowed (must be %s)", parsed.Host, rc.allowedHost)
	}
	if parsed.Scheme != rc.allowedScheme {
		return fmt.Errorf("download URL scheme %q not allowed (must be %s)", parsed.Scheme, rc.allowedScheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("download URL must not contain userinfo: %s", rawURL)
	}
	if strings.Contains(parsed.Path, "..") {
		return fmt.Errorf("download: URL contains path traversal: %s", rawURL)
	}
	if rc.allowedBasePath != "" && !strings.HasPrefix(parsed.Path, rc.allowedBasePath) {
		return fmt.Errorf("download URL path %q not under allowed base %q", parsed.Path, rc.allowedBasePath)
	}
	return nil
}

// errHTTPNotFound indicates a 404 response.
var errHTTPNotFound = errors.New("HTTP 404")

// isHTTPNotFound reports whether err wraps errHTTPNotFound.
func isHTTPNotFound(err error) bool {
	return errors.Is(err, errHTTPNotFound)
}

// fetchToFile downloads a single URL to a local file path.
// Enforces a 10MB size limit. Returns errHTTPNotFound on 404, other errors on failure.
func (rc *RegistryClient) fetchToFile(fileURL, dest string) error {
	resp, err := rc.client.Get(fileURL)
	if err != nil {
		return fmt.Errorf("GET %s: %w", fileURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", fileURL, errHTTPNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, fileURL)
	}

	const maxSize = 10 << 20 // 10MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxSize {
		return fmt.Errorf("response body exceeds %d bytes", maxSize)
	}

	return os.WriteFile(dest, body, 0o600)
}

// FilterEntries returns entries whose ID, Name, or Description contain the
// query as a case-insensitive substring. An empty query returns all entries.
func FilterEntries(entries []RegistryEntry, query string) []RegistryEntry {
	if query == "" {
		return entries
	}
	q := strings.ToLower(query)
	var out []RegistryEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.ID), q) ||
			strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Description), q) {
			out = append(out, e)
		}
	}
	return out
}

// FindEntry fetches the registry index and returns the entry matching the given ID.
func (rc *RegistryClient) FindEntry(id string) (*RegistryEntry, error) {
	entries, err := rc.FetchIndex()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("package %q not found in registry", id)
}

// SearchEntries fetches the registry index and filters by query.
func (rc *RegistryClient) SearchEntries(query string) ([]RegistryEntry, error) {
	entries, err := rc.FetchIndex()
	if err != nil {
		return nil, err
	}
	return FilterEntries(entries, query), nil
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

// SearchEntries filters registry entries by keyword, matching against ID, Name,
// and Description (case-insensitive).
func SearchEntries(entries []RegistryEntry, keyword string) []RegistryEntry {
	if keyword == "" {
		return entries
	}
	kw := strings.ToLower(keyword)
	var results []RegistryEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.ID), kw) ||
			strings.Contains(strings.ToLower(e.Name), kw) ||
			strings.Contains(strings.ToLower(e.Description), kw) {
			results = append(results, e)
		}
	}
	return results
}
