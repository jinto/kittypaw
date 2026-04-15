package engine

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// htmlToMarkdown converts an HTML string to Markdown using the
// golang.org/x/net/html tokenizer. It handles headings, paragraphs, links,
// lists, code blocks, and tables. Script and style content is discarded.
func htmlToMarkdown(htmlContent string) string {
	z := html.NewTokenizer(strings.NewReader(htmlContent))
	var (
		out   strings.Builder
		stack []atom.Atom // open tag stack

		// link state
		linkHref string
		linkText strings.Builder

		// list state
		olCounter int
		listDepth int

		// code block state
		inPre  bool
		inCode bool

		// table state
		inTable   bool
		tableRows [][]string // collected rows
		tableRow  []string   // current row cells
		cellBuf   strings.Builder

		// skip content inside <script> / <style>
		skipDepth int
	)

	inLink := func() bool { return linkHref != "" }

	flushText := func(s string) {
		if skipDepth > 0 {
			return
		}
		if inTable {
			cellBuf.WriteString(s)
			return
		}
		if inLink() {
			linkText.WriteString(s)
			return
		}
		out.WriteString(s)
	}

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			goto done

		case html.TextToken:
			text := z.Text()
			s := string(text)
			if inPre || inCode {
				flushText(s)
			} else {
				// Collapse whitespace outside pre/code
				s = collapseWhitespace(s)
				if s != "" {
					flushText(s)
				}
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := z.TagName()
			a := atom.Lookup(tn)

			// Skip script/style
			if a == atom.Script || a == atom.Style {
				if tt == html.StartTagToken {
					skipDepth++
				}
				continue
			}
			if skipDepth > 0 {
				continue
			}

			switch a {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				ensureNewline(&out)
				level := int(tn[1] - '0')
				out.WriteString(strings.Repeat("#", level))
				out.WriteByte(' ')
				stack = append(stack, a)

			case atom.P, atom.Div, atom.Article, atom.Section, atom.Header, atom.Footer, atom.Main:
				ensureDoubleNewline(&out)
				stack = append(stack, a)

			case atom.Br:
				out.WriteString("\n")

			case atom.A:
				href := getAttr(z, hasAttr, "href")
				linkHref = href
				linkText.Reset()
				stack = append(stack, a)

			case atom.Strong, atom.B:
				flushText("**")
				stack = append(stack, a)

			case atom.Em, atom.I:
				flushText("*")
				stack = append(stack, a)

			case atom.Ul:
				if listDepth == 0 {
					ensureNewline(&out)
				}
				listDepth++
				stack = append(stack, a)

			case atom.Ol:
				if listDepth == 0 {
					ensureNewline(&out)
				}
				olCounter = 0
				listDepth++
				stack = append(stack, a)

			case atom.Li:
				ensureNewline(&out)
				indent := strings.Repeat("  ", listDepth-1)
				parent := findParent(stack, atom.Ul, atom.Ol)
				if parent == atom.Ol {
					olCounter++
					out.WriteString(fmt.Sprintf("%s%d. ", indent, olCounter))
				} else {
					out.WriteString(indent + "- ")
				}
				stack = append(stack, a)

			case atom.Pre:
				ensureNewline(&out)
				inPre = true
				stack = append(stack, a)

			case atom.Code:
				if inPre {
					lang := getAttr(z, hasAttr, "class")
					lang = extractLang(lang)
					out.WriteString("```" + lang + "\n")
					inCode = true
				} else {
					flushText("`")
				}
				stack = append(stack, a)

			case atom.Table:
				ensureNewline(&out)
				inTable = true
				tableRows = nil
				stack = append(stack, a)

			case atom.Tr:
				tableRow = nil
				stack = append(stack, a)

			case atom.Th, atom.Td:
				cellBuf.Reset()
				stack = append(stack, a)

			case atom.Blockquote:
				ensureNewline(&out)
				out.WriteString("> ")
				stack = append(stack, a)

			case atom.Hr:
				ensureNewline(&out)
				out.WriteString("---\n")

			default:
				if tt == html.StartTagToken {
					stack = append(stack, a)
				}
			}

		case html.EndTagToken:
			tn, _ := z.TagName()
			a := atom.Lookup(tn)

			// Skip script/style
			if a == atom.Script || a == atom.Style {
				if skipDepth > 0 {
					skipDepth--
				}
				continue
			}
			if skipDepth > 0 {
				continue
			}

			switch a {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				out.WriteString("\n\n")
				popStack(&stack, a)

			case atom.P, atom.Div, atom.Article, atom.Section, atom.Header, atom.Footer, atom.Main:
				ensureDoubleNewline(&out)
				popStack(&stack, a)

			case atom.A:
				if inLink() {
					text := strings.TrimSpace(linkText.String())
					if text == "" {
						text = linkHref
					}
					href := linkHref
					linkHref = ""
					flushText(fmt.Sprintf("[%s](%s)", text, href))
				}
				popStack(&stack, a)

			case atom.Strong, atom.B:
				flushText("**")
				popStack(&stack, a)

			case atom.Em, atom.I:
				flushText("*")
				popStack(&stack, a)

			case atom.Ul, atom.Ol:
				listDepth--
				if listDepth == 0 {
					ensureNewline(&out)
				}
				if a == atom.Ol {
					olCounter = 0
				}
				popStack(&stack, a)

			case atom.Li:
				popStack(&stack, a)

			case atom.Code:
				if inPre {
					// Ensure exactly one trailing newline before closing fence
					s := out.String()
					s = strings.TrimRight(s, "\n")
					out.Reset()
					out.WriteString(s)
					out.WriteString("\n```\n")
					inCode = false
				} else {
					flushText("`")
				}
				popStack(&stack, a)

			case atom.Pre:
				inPre = false
				ensureNewline(&out)
				popStack(&stack, a)

			case atom.Th, atom.Td:
				tableRow = append(tableRow, strings.TrimSpace(cellBuf.String()))
				cellBuf.Reset()
				popStack(&stack, a)

			case atom.Tr:
				if len(tableRow) > 0 {
					tableRows = append(tableRows, tableRow)
				}
				tableRow = nil
				popStack(&stack, a)

			case atom.Table:
				inTable = false
				renderTable(&out, tableRows)
				tableRows = nil
				popStack(&stack, a)

			case atom.Blockquote:
				ensureNewline(&out)
				popStack(&stack, a)

			default:
				popStack(&stack, a)
			}
		}
	}

done:
	result := out.String()
	// Normalize excessive newlines
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}

// extractTitle extracts the text content of the first <title> tag.
func extractTitle(htmlContent string) string {
	z := html.NewTokenizer(strings.NewReader(htmlContent))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			tn, _ := z.TagName()
			if atom.Lookup(tn) == atom.Title {
				if z.Next() == html.TextToken {
					return strings.TrimSpace(string(z.Text()))
				}
				return ""
			}
		}
	}
}

// --- helpers ---

func ensureNewline(b *strings.Builder) {
	s := b.String()
	if len(s) > 0 && s[len(s)-1] != '\n' {
		b.WriteByte('\n')
	}
}

func ensureDoubleNewline(b *strings.Builder) {
	s := b.String()
	if len(s) == 0 {
		return
	}
	if !strings.HasSuffix(s, "\n\n") {
		if strings.HasSuffix(s, "\n") {
			b.WriteByte('\n')
		} else {
			b.WriteString("\n\n")
		}
	}
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prev {
				b.WriteByte(' ')
				prev = true
			}
		} else {
			b.WriteRune(r)
			prev = false
		}
	}
	return b.String()
}

func getAttr(z *html.Tokenizer, hasAttr bool, key string) string {
	if !hasAttr {
		return ""
	}
	for {
		k, v, more := z.TagAttr()
		if string(k) == key {
			return string(v)
		}
		if !more {
			break
		}
	}
	return ""
}

func extractLang(class string) string {
	// "language-go" or "lang-go" → "go"
	for _, prefix := range []string{"language-", "lang-"} {
		if strings.HasPrefix(class, prefix) {
			return strings.TrimPrefix(class, prefix)
		}
	}
	return ""
}

func findParent(stack []atom.Atom, targets ...atom.Atom) atom.Atom {
	for i := len(stack) - 1; i >= 0; i-- {
		for _, t := range targets {
			if stack[i] == t {
				return t
			}
		}
	}
	return 0
}

func popStack(stack *[]atom.Atom, a atom.Atom) {
	for i := len(*stack) - 1; i >= 0; i-- {
		if (*stack)[i] == a {
			*stack = append((*stack)[:i], (*stack)[i+1:]...)
			return
		}
	}
}

func renderTable(out *strings.Builder, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	// Determine column count from the widest row
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return
	}

	// Normalize rows to have equal columns
	for i, row := range rows {
		for len(row) < cols {
			row = append(row, "")
		}
		rows[i] = row
	}

	// Calculate column widths
	widths := make([]int, cols)
	for _, row := range rows {
		for j, cell := range row {
			if len(cell) > widths[j] {
				widths[j] = len(cell)
			}
		}
	}
	for i, w := range widths {
		if w < 3 {
			widths[i] = 3
		}
	}

	// Render header row
	renderTableRow(out, rows[0], widths)

	// Render separator
	out.WriteByte('|')
	for _, w := range widths {
		out.WriteByte(' ')
		out.WriteString(strings.Repeat("-", w))
		out.WriteString(" |")
	}
	out.WriteByte('\n')

	// Render data rows
	for _, row := range rows[1:] {
		renderTableRow(out, row, widths)
	}
}

func renderTableRow(out *strings.Builder, row []string, widths []int) {
	out.WriteByte('|')
	for j, cell := range row {
		out.WriteByte(' ')
		out.WriteString(cell)
		out.WriteString(strings.Repeat(" ", widths[j]-len(cell)))
		out.WriteString(" |")
	}
	out.WriteByte('\n')
}
