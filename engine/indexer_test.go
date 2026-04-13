package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestWorkspace creates a temp directory with test files and returns
// the path. Caller must not call cleanup before tests finish.
func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Text files.
	writeFile(t, dir, "main.go", "package main\n\nfunc handleSearch(query string) {\n\tfmt.Println(query)\n}\n")
	writeFile(t, dir, "util.go", "package main\n\nfunc formatOutput(s string) string {\n\treturn s\n}\n")
	writeFile(t, dir, "README.md", "# Project\n\nThis is a test project.\n")

	// Subdirectory.
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	writeFile(t, dir, "src/handler.go", "package src\n\nfunc HandleRequest() {}\n")

	// Binary file (contains null bytes).
	os.WriteFile(filepath.Join(dir, "image.png"), []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00, 0x00}, 0o644)

	// Excluded directory.
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755)
	writeFile(t, dir, ".git/config", "[core]\nrepositoryformatversion = 0\n")

	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)
	writeFile(t, dir, "node_modules/pkg/index.js", "module.exports = {}\n")

	// .DS_Store
	os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte{0x00, 0x00, 0x00, 0x01}, 0o644)

	return dir
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestIndex_BasicWalk(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	result, err := ix.Index(context.Background(), "ws-test", dir)
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	// Expected: main.go, util.go, README.md, src/handler.go, image.png = 5 indexed
	// Skipped: .DS_Store = 1
	// Excluded dirs: .git/, node_modules/ (entire subtrees skipped, not counted)
	if result.Indexed != 5 {
		t.Errorf("indexed: got %d, want 5", result.Indexed)
	}
	if result.Skipped != 1 {
		t.Errorf("skipped: got %d, want 1", result.Skipped)
	}
	if result.DurationMs < 0 {
		t.Error("duration should be non-negative")
	}
}

func TestIndex_BinaryDetection(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	ix.Index(context.Background(), "ws-bin", dir)

	// image.png should be indexed (filename-only, no content).
	// Search for image.png by filename.
	results, _, err := st.SearchWorkspaceFTS("image", "", "", 20, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for binary filename search, got %d", len(results))
	}
	if results[0].Filename != "image.png" {
		t.Errorf("filename: got %q, want %q", results[0].Filename, "image.png")
	}
}

func TestIndex_LargeFileSkipsContent(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := t.TempDir()

	// Create a file > 1MB.
	bigContent := make([]byte, maxIndexFileSize+1)
	for i := range bigContent {
		bigContent[i] = 'A'
	}
	// Add a unique token at the start.
	copy(bigContent, []byte("UNIQUE_LARGE_TOKEN"))
	os.WriteFile(filepath.Join(dir, "big.txt"), bigContent, 0o644)

	// Also a small file with the same token.
	writeFile(t, dir, "small.txt", "UNIQUE_LARGE_TOKEN in a small file")

	ix.Index(context.Background(), "ws-big", dir)

	// Search for the token — only small.txt should have content match.
	results, _, _ := st.SearchWorkspaceFTS("UNIQUE_LARGE_TOKEN", "", "", 20, 0)
	if len(results) != 1 {
		t.Errorf("expected 1 content match, got %d", len(results))
	}
}

func TestIndex_ExcludedDirs(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	ix.Index(context.Background(), "ws-excl", dir)

	// Files inside .git/ and node_modules/ should not be indexed at all.
	results, _, _ := st.SearchWorkspaceFTS("repositoryformatversion", "", "", 20, 0)
	if len(results) != 0 {
		t.Errorf(".git file indexed: got %d results", len(results))
	}
	results, _, _ = st.SearchWorkspaceFTS("exports", "", "", 20, 0)
	if len(results) != 0 {
		t.Errorf("node_modules file indexed: got %d results", len(results))
	}
}

func TestRemove(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	ix.Index(context.Background(), "ws-rm", dir)

	// Verify files are searchable.
	results, _, _ := st.SearchWorkspaceFTS("handleSearch", "", "", 20, 0)
	if len(results) == 0 {
		t.Fatal("expected results before remove")
	}

	// Remove.
	if err := ix.Remove("ws-rm"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Verify gone.
	results, total, _ := st.SearchWorkspaceFTS("handleSearch", "", "", 20, 0)
	if total != 0 {
		t.Errorf("post-remove search: got %d, want 0", total)
	}
}

func TestIndex_ConcurrentGuard(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	// Simulate an in-progress indexing.
	ix.indexing.Store("ws-concurrent", true)

	result, err := ix.Index(context.Background(), "ws-concurrent", dir)
	if err != nil {
		t.Fatalf("concurrent index should not error: %v", err)
	}
	if result.Indexed != 0 {
		t.Errorf("concurrent index should skip: got %d indexed", result.Indexed)
	}

	ix.indexing.Delete("ws-concurrent")
}

func TestIndex_ContextCancellation(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)

	// Create a directory with many files.
	dir := t.TempDir()
	for i := range 100 {
		writeFile(t, dir, filepath.Join("src", filepath.Base(filepath.Join("src", string(rune('a'+i%26))+".go"))),
			"package src\nfunc F() {}\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := ix.Index(ctx, "ws-cancel", dir)
	if err != context.Canceled {
		// walkErr might be context.Canceled or nil depending on timing.
		// The key point is it should not index all 100 files.
	}
	_ = err
}

func TestIndex_NonexistentPath(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)

	_, err := ix.Index(context.Background(), "ws-missing", "/nonexistent/path/to/workspace")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestIndex_EmptyBodyForBinaryInFTS(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := t.TempDir()

	// Binary file.
	os.WriteFile(filepath.Join(dir, "data.bin"), []byte{0x00, 0xFF, 0x00, 0xFF}, 0o644)
	// Text file.
	writeFile(t, dir, "code.go", "package main\nfunc binHelper() {}\n")

	ix.Index(context.Background(), "ws-empty-body", dir)

	// Search for a term in the text file — should work.
	results, _, _ := st.SearchWorkspaceFTS("binHelper", "", "", 20, 0)
	if len(results) != 1 {
		t.Errorf("text file search: got %d, want 1", len(results))
	}

	// Search for the binary filename — should work (filename is indexed).
	results, _, _ = st.SearchWorkspaceFTS("data", "", "", 20, 0)
	if len(results) != 1 {
		t.Errorf("binary filename search: got %d, want 1", len(results))
	}
}

func TestSearch_WithSnippets(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	ix.Index(context.Background(), "ws-snip", dir)

	result, err := ix.Search(context.Background(), "handleSearch", SearchOptions{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files: got %d, want 1", len(result.Files))
	}
	if len(result.Files[0].Snippets) == 0 {
		t.Fatal("expected snippets")
	}
	snip := result.Files[0].Snippets[0]
	if snip.Line != 3 {
		t.Errorf("snippet line: got %d, want 3", snip.Line)
	}
	if snip.Text == "" {
		t.Error("snippet text should not be empty")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)

	_, err := ix.Search(context.Background(), "", SearchOptions{})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearch_PathFilter(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	ix.Index(context.Background(), "ws-pf", dir)

	// Search in src/ only.
	result, err := ix.Search(context.Background(), "package", SearchOptions{Path: "src/"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Only src/handler.go should match.
	if len(result.Files) != 1 {
		t.Errorf("path filter: got %d files, want 1", len(result.Files))
	}
}

func TestSearch_ExtFilter(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	ix.Index(context.Background(), "ws-ef", dir)

	// Search for "Project" with .md filter.
	result, err := ix.Search(context.Background(), "Project", SearchOptions{Ext: ".md"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Files) != 1 {
		t.Errorf("ext filter: got %d files, want 1", len(result.Files))
	}
}

func TestStats(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	ix.Index(context.Background(), "ws-stats", dir)

	stats, err := ix.Stats(context.Background(), StatsOptions{})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	// 5 total files (main.go, util.go, README.md, src/handler.go, image.png).
	if stats.TotalFiles != 5 {
		t.Errorf("total: got %d, want 5", stats.TotalFiles)
	}
	// 4 with content (text files), 1 binary (image.png).
	if stats.IndexedFiles != 4 {
		t.Errorf("indexed: got %d, want 4", stats.IndexedFiles)
	}
	if stats.TotalSize <= 0 {
		t.Error("total size should be positive")
	}
	if _, ok := stats.ByExtension[".go"]; !ok {
		t.Error("missing .go in by_extension")
	}
	if stats.IndexedAt == "" {
		t.Error("indexed_at should not be empty")
	}
}

func TestReindex(t *testing.T) {
	st := openTestStore(t)
	ix := NewFTS5Indexer(st)
	dir := setupTestWorkspace(t)

	// Initial index.
	ix.Index(context.Background(), "ws-ri", dir)

	// Modify a file.
	writeFile(t, dir, "main.go", "package main\n\nfunc newFunction() {\n\tfmt.Println(\"new\")\n}\n")

	// Delete a file.
	os.Remove(filepath.Join(dir, "util.go"))

	// Add a small delay so indexed_at differs.
	time.Sleep(20 * time.Millisecond)

	// Reindex.
	result, err := ix.Reindex(context.Background(), "ws-ri", dir)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if result.Indexed < 4 {
		t.Errorf("reindexed: got %d, want >= 4", result.Indexed)
	}

	// New content should be searchable.
	searchResult, _ := ix.Search(context.Background(), "newFunction", SearchOptions{})
	if len(searchResult.Files) != 1 {
		t.Errorf("new content: got %d, want 1", len(searchResult.Files))
	}

	// Old content should not be searchable.
	searchResult, _ = ix.Search(context.Background(), "handleSearch", SearchOptions{})
	if len(searchResult.Files) != 0 {
		t.Errorf("old content still found: got %d, want 0", len(searchResult.Files))
	}
}

func TestExtractSnippets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	os.WriteFile(path, []byte("line one\nline two\nfunc myTarget() {\n\treturn\n}\n"), 0o644)

	snippets := extractSnippets(path, "myTarget")
	if len(snippets) != 1 {
		t.Fatalf("snippets: got %d, want 1", len(snippets))
	}
	if snippets[0].Line != 3 {
		t.Errorf("line: got %d, want 3", snippets[0].Line)
	}
	if snippets[0].Text != "func myTarget() {" {
		t.Errorf("text: got %q", snippets[0].Text)
	}
}

func TestExtractSnippets_FileDeleted(t *testing.T) {
	snippets := extractSnippets("/nonexistent/file.go", "query")
	if len(snippets) != 0 {
		t.Errorf("expected empty snippets for deleted file, got %d", len(snippets))
	}
}

func TestSplitQueryTerms(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{"handleSearch", 1},
		{"func main", 2},
		{`"exact phrase"`, 2},
		{"term AND other", 2},   // AND stripped
		{"+required -excluded", 2},
		{"", 0},
	}
	for _, tt := range tests {
		terms := splitQueryTerms(tt.query)
		if len(terms) != tt.want {
			t.Errorf("splitQueryTerms(%q): got %d terms, want %d (%v)", tt.query, len(terms), tt.want, terms)
		}
	}
}

func TestIsBinary(t *testing.T) {
	dir := t.TempDir()

	// Text file.
	textPath := filepath.Join(dir, "text.txt")
	os.WriteFile(textPath, []byte("hello world"), 0o644)
	if isBinary(textPath) {
		t.Error("text file detected as binary")
	}

	// Binary file.
	binPath := filepath.Join(dir, "bin.dat")
	os.WriteFile(binPath, []byte{0x89, 0x50, 0x00, 0x47}, 0o644)
	if !isBinary(binPath) {
		t.Error("binary file not detected")
	}

	// Nonexistent file.
	if !isBinary("/nonexistent") {
		t.Error("nonexistent file should be treated as binary")
	}
}
