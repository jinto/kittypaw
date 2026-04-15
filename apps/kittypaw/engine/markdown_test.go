package engine

import (
	"strings"
	"testing"
)

func TestHTMLToMarkdown_Headings(t *testing.T) {
	tests := []struct {
		html string
		want string
	}{
		{"<h1>Title</h1>", "# Title"},
		{"<h2>Sub</h2>", "## Sub"},
		{"<h3>Third</h3>", "### Third"},
		{"<h4>Fourth</h4>", "#### Fourth"},
		{"<h5>Fifth</h5>", "##### Fifth"},
		{"<h6>Sixth</h6>", "###### Sixth"},
	}
	for _, tc := range tests {
		got := htmlToMarkdown(tc.html)
		if got != tc.want {
			t.Errorf("htmlToMarkdown(%q) = %q, want %q", tc.html, got, tc.want)
		}
	}
}

func TestHTMLToMarkdown_Paragraphs(t *testing.T) {
	got := htmlToMarkdown("<p>First paragraph.</p><p>Second paragraph.</p>")
	if !strings.Contains(got, "First paragraph.") || !strings.Contains(got, "Second paragraph.") {
		t.Errorf("expected both paragraphs, got: %q", got)
	}
	// Should have double newline between paragraphs
	if !strings.Contains(got, "\n\n") {
		t.Errorf("expected double newline between paragraphs, got: %q", got)
	}
}

func TestHTMLToMarkdown_Links(t *testing.T) {
	tests := []struct {
		html string
		want string
	}{
		{`<a href="https://example.com">Example</a>`, "[Example](https://example.com)"},
		{`<a href="https://example.com"></a>`, "[https://example.com](https://example.com)"},
	}
	for _, tc := range tests {
		got := htmlToMarkdown(tc.html)
		if got != tc.want {
			t.Errorf("htmlToMarkdown(%q) = %q, want %q", tc.html, got, tc.want)
		}
	}
}

func TestHTMLToMarkdown_UnorderedList(t *testing.T) {
	html := "<ul><li>One</li><li>Two</li><li>Three</li></ul>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "- One") || !strings.Contains(got, "- Two") || !strings.Contains(got, "- Three") {
		t.Errorf("expected unordered list items, got: %q", got)
	}
}

func TestHTMLToMarkdown_OrderedList(t *testing.T) {
	html := "<ol><li>First</li><li>Second</li><li>Third</li></ol>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "1. First") || !strings.Contains(got, "2. Second") || !strings.Contains(got, "3. Third") {
		t.Errorf("expected ordered list items, got: %q", got)
	}
}

func TestHTMLToMarkdown_CodeBlock(t *testing.T) {
	html := `<pre><code class="language-go">func main() {
    fmt.Println("hello")
}</code></pre>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "```go") {
		t.Errorf("expected fenced code block with language, got: %q", got)
	}
	if !strings.Contains(got, `fmt.Println("hello")`) {
		t.Errorf("expected code content preserved, got: %q", got)
	}
	if !strings.HasSuffix(got, "\n```") {
		t.Errorf("expected closing fence, got: %q", got)
	}
}

func TestHTMLToMarkdown_InlineCode(t *testing.T) {
	html := "<p>Use <code>fmt.Println</code> for output.</p>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "`fmt.Println`") {
		t.Errorf("expected inline code, got: %q", got)
	}
}

func TestHTMLToMarkdown_Table(t *testing.T) {
	html := `<table>
		<tr><th>Name</th><th>Age</th></tr>
		<tr><td>Alice</td><td>30</td></tr>
		<tr><td>Bob</td><td>25</td></tr>
	</table>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "| Name") {
		t.Errorf("expected table header, got: %q", got)
	}
	if !strings.Contains(got, "| ---") || !strings.Contains(got, "---") {
		t.Errorf("expected table separator, got: %q", got)
	}
	if !strings.Contains(got, "| Alice") {
		t.Errorf("expected table data, got: %q", got)
	}
}

func TestHTMLToMarkdown_ScriptStyleIgnored(t *testing.T) {
	html := `<p>Hello</p><script>alert("xss")</script><style>.hidden{display:none}</style><p>World</p>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "alert") {
		t.Errorf("script content should be ignored, got: %q", got)
	}
	if strings.Contains(got, "display") {
		t.Errorf("style content should be ignored, got: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("non-script content should be preserved, got: %q", got)
	}
}

func TestHTMLToMarkdown_BoldItalic(t *testing.T) {
	html := "<p><strong>bold</strong> and <em>italic</em></p>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "**bold**") {
		t.Errorf("expected bold, got: %q", got)
	}
	if !strings.Contains(got, "*italic*") {
		t.Errorf("expected italic, got: %q", got)
	}
}

func TestHTMLToMarkdown_NestedList(t *testing.T) {
	html := `<ul>
		<li>Parent
			<ul><li>Child</li></ul>
		</li>
	</ul>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "- Parent") {
		t.Errorf("expected parent list item, got: %q", got)
	}
	if !strings.Contains(got, "  - Child") {
		t.Errorf("expected indented child list item, got: %q", got)
	}
}

func TestHTMLToMarkdown_NestedLinkInList(t *testing.T) {
	html := `<ul><li><a href="https://example.com">Link text</a></li></ul>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "[Link text](https://example.com)") {
		t.Errorf("expected link in list item, got: %q", got)
	}
}

func TestHTMLToMarkdown_EmptyInput(t *testing.T) {
	got := htmlToMarkdown("")
	if got != "" {
		t.Errorf("expected empty output for empty input, got: %q", got)
	}
}

func TestHTMLToMarkdown_MalformedHTML(t *testing.T) {
	// Unclosed tags — should not panic
	html := "<p>Unclosed paragraph<h2>Also unclosed<div>And this"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "Unclosed paragraph") {
		t.Errorf("expected content from malformed HTML, got: %q", got)
	}
}

func TestHTMLToMarkdown_HTMLEntities(t *testing.T) {
	html := "<p>A &amp; B &lt; C &gt; D &quot;quoted&quot;</p>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "A & B") {
		t.Errorf("expected decoded entities, got: %q", got)
	}
}

func TestHTMLToMarkdown_Blockquote(t *testing.T) {
	html := "<blockquote>Wise words</blockquote>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "> Wise words") {
		t.Errorf("expected blockquote, got: %q", got)
	}
}

func TestHTMLToMarkdown_HorizontalRule(t *testing.T) {
	html := "<p>Above</p><hr><p>Below</p>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "---") {
		t.Errorf("expected horizontal rule, got: %q", got)
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		html string
		want string
	}{
		{"<html><head><title>My Page</title></head><body></body></html>", "My Page"},
		{"<title>  Spaced  </title>", "Spaced"},
		{"<html><body>No title here</body></html>", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := extractTitle(tc.html)
		if got != tc.want {
			t.Errorf("extractTitle(%q) = %q, want %q", tc.html, got, tc.want)
		}
	}
}
